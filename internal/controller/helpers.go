package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ptr[T any](v T) *T { return &v }

func scanJobNameWithTimestamp(scanName string, creationTimestamp time.Time) string {
	h := sha256.Sum256([]byte(scanName))
	ts := creationTimestamp.Unix()
	return fmt.Sprintf("zap-scan-%s-%d", hex.EncodeToString(h[:])[:8], ts)
}

func jobNamespaceFor(scanNs string, jobNamespace *string) string {
	if jobNamespace != nil && *jobNamespace != "" {
		return *jobNamespace
	}
	return scanNs
}

func buildZapFullScanJob(jobName, scanNamespace, scanName string, specTarget string, openapi *string, image *string, extraArgs []string, saName *string) *batchv1.Job {
	img := "ghcr.io/zaproxy/zaproxy:stable"
	if image != nil && *image != "" {
		img = *image
	}

	name := jobName

	// We use zap-full-scan.py inside the official image.
	// It expects /zap/wrk to exist and be writable when file outputs are configured.
	args := []string{"zap-full-scan.py", "-t", specTarget, "-J", "/zap/wrk/zap.json", "-r", "/zap/wrk/zap.html", "-d"}
	if openapi != nil && *openapi != "" {
		args = append(args, "-O", *openapi)
	}
	args = append(args, extraArgs...)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: scanNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "zap-operator",
				"app.kubernetes.io/component": "zap-scan",
				"spaceship.com/scan-name":     scanName,
				"spaceship.com/scan-ns":       scanNamespace,
				"spaceship.com/scan":          "true",
				"spaceship.com/scan-owner":    scanName,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr[int32](0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"spaceship.com/scan": "true",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Volumes: []corev1.Volume{
						{
							Name:         "zap-wrk",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
						{
							Name:         "zap-home",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "zap",
							Image:           img,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/bin/sh", "-c"},
							Args: []string{
								// ZAP exits with 1/2/3 when alerts are found (by severity).
								// We treat these as success since finding alerts is expected behavior.
								// Only propagate exit codes > 3 which indicate real errors.
								"mkdir -p /zap/wrk && python3 /zap/" + joinShell(args) + "; ec=$?; if [ $ec -le 3 ]; then exit 0; else exit $ec; fi",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "zap-wrk",
									MountPath: "/zap/wrk",
								},
								{
									Name:      "zap-home",
									MountPath: "/home/zap/.ZAP",
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("6Gi"),
								},
							},
						},
						{
							Name:            "reporter",
							Image:           "busybox:1.36",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/bin/sh", "-c"},
							Args: []string{
								"set -eu; echo 'zap-operator: waiting for /zap/wrk/zap.json'; while [ ! -f /zap/wrk/zap.json ]; do sleep 2; done; echo 'zap-operator: begin zap.json'; cat /zap/wrk/zap.json; echo; echo 'zap-operator: end zap.json';",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "zap-wrk",
									MountPath: "/zap/wrk",
								},
								{
									Name:      "zap-home",
									MountPath: "/home/zap/.ZAP",
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
		},
	}

	if saName != nil && *saName != "" {
		job.Spec.Template.Spec.ServiceAccountName = *saName
	}

	return job
}

func joinShell(args []string) string {
	// Minimal quoting: wrap args with spaces in single quotes.
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		needs := false
		for _, ch := range a {
			if ch == ' ' || ch == '\t' || ch == '\n' {
				needs = true
				break
			}
		}
		if needs {
			out += "'" + a + "'"
		} else {
			out += a
		}
	}
	return out
}

func isJobComplete(job *batchv1.Job) (complete bool, succeeded bool) {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true, true
		}
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true, false
		}
	}
	return false, false
}

func jobFinishedTime(job *batchv1.Job) *metav1.Time {
	if job.Status.CompletionTime != nil {
		return job.Status.CompletionTime
	}
	// fallback: infer from conditions.
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && !c.LastTransitionTime.IsZero() {
			t := c.LastTransitionTime
			return &t
		}
	}
	return nil
}
