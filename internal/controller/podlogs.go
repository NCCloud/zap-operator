package controller

import (
	"context"
	"fmt"
	"io"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *ScanReconciler) podsForJob(ctx context.Context, job *batchv1.Job) (*corev1.PodList, error) {
	// Job's pods have label job-name
	var pods corev1.PodList
	selector := labels.Set(map[string]string{"job-name": job.Name}).AsSelector()
	if err := r.List(ctx, &pods, &client.ListOptions{Namespace: job.Namespace, LabelSelector: selector}); err != nil {
		return nil, err
	}
	return &pods, nil
}

func (r *ScanReconciler) getPodLogs(ctx context.Context, namespace, podName, container string) ([]byte, error) {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	req := cs.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{Container: container})
	stream, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	b, err := io.ReadAll(stream)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func jobKey(job *batchv1.Job) types.NamespacedName {
	return types.NamespacedName{Name: job.Name, Namespace: job.Namespace}
}

func formatNN(nn types.NamespacedName) string {
	if nn.Namespace == "" {
		return nn.Name
	}
	return fmt.Sprintf("%s/%s", nn.Namespace, nn.Name)
}
