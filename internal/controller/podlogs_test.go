package controller

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPodsForJob_Success(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns1"},
	}

	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-1",
			Namespace: "ns1",
			Labels:    map[string]string{"job-name": "test-job"},
		},
	}

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-2",
			Namespace: "ns1",
			Labels:    map[string]string{"job-name": "test-job"},
		},
	}

	// Pod from different job - should not be included
	pod3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-pod",
			Namespace: "ns1",
			Labels:    map[string]string{"job-name": "other-job"},
		},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod1, pod2, pod3).Build(),
		Scheme: s,
	}

	pods, err := r.podsForJob(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods.Items) != 2 {
		t.Errorf("expected 2 pods, got %d", len(pods.Items))
	}
}

func TestPodsForJob_NoPods(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns1"},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job).Build(),
		Scheme: s,
	}

	pods, err := r.podsForJob(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Errorf("expected 0 pods, got %d", len(pods.Items))
	}
}

func TestPodsForJob_DifferentNamespace(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns1"},
	}

	// Pod in different namespace - should not be included
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "ns2",
			Labels:    map[string]string{"job-name": "test-job"},
		},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
	}

	pods, err := r.podsForJob(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Errorf("expected 0 pods (different namespace), got %d", len(pods.Items))
	}
}

func TestPodsForJob_ListError(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	// Don't add corev1 Pod type to scheme to trigger list error
	if err := batchv1.AddToScheme(s); err != nil {
		t.Fatalf("add batch scheme: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns1"},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).Build(),
		Scheme: s,
	}

	_, err := r.podsForJob(ctx, job)
	if err == nil {
		t.Error("expected error when listing pods fails")
	}
}
