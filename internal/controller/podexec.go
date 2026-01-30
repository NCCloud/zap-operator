package controller

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *ScanReconciler) execInPod(ctx context.Context, namespace, podName, container string, command []string) ([]byte, []byte, error) {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, err
	}

	req := cs.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   command,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return nil, nil, err
	}

	var stdout, stderr bytes.Buffer
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		return stdout.Bytes(), stderr.Bytes(), err
	}

	return stdout.Bytes(), stderr.Bytes(), nil
}

func (r *ScanReconciler) readFileFromPod(ctx context.Context, namespace, podName, container, filePath string) ([]byte, error) {
	cmd := []string{"/bin/sh", "-c", fmt.Sprintf("test -f %q && cat %q", filePath, filePath)}

	er := r.execRunner
	if er == nil {
		er = r
	}
	stdout, stderr, err := er.execInPod(ctx, namespace, podName, container, cmd)
	if err != nil {
		return nil, fmt.Errorf("exec failed: %w: %s", err, string(stderr))
	}
	if len(stdout) == 0 {
		return nil, fmt.Errorf("file not found or empty: %s (stderr=%s)", filePath, string(stderr))
	}
	return stdout, nil
}
