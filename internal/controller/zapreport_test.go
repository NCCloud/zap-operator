package controller

import (
	"context"
	"errors"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// podExecGetterFunc is a test helper that implements podExecGetter interface
type podExecGetterFunc func(ctx context.Context, namespace, podName, container, filePath string) ([]byte, error)

func (f podExecGetterFunc) readFileFromPod(ctx context.Context, namespace, podName, container, filePath string) ([]byte, error) {
	return f(ctx, namespace, podName, container, filePath)
}

func TestCollectAlertsFromPodReport_NoPods(t *testing.T) {
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

	alerts, err := r.collectAlertsFromPodReport(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alerts.Total != 0 {
		t.Errorf("expected 0 alerts, got %d", alerts.Total)
	}
}

func TestCollectAlertsFromPodReport_NoRunningPods(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns1"},
	}

	// Pod in non-Running phase
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "ns1",
			Labels:    map[string]string{"job-name": "test-job"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
	}

	alerts, err := r.collectAlertsFromPodReport(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alerts.Total != 0 {
		t.Errorf("expected 0 alerts for non-running pod, got %d", alerts.Total)
	}
}

func TestCollectAlertsFromPodReport_RunningPodWithAlerts(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns1"},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "ns1",
			Labels:    map[string]string{"job-name": "test-job"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	// JSON report with alerts
	reportJSON := `{
		"site": [{
			"alerts": [
				{"pluginid": "10001", "riskcode": "2"},
				{"pluginid": "10002", "riskcode": "3"},
				{"pluginid": "10001", "riskcode": "2"}
			]
		}]
	}`

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
		execGetter: podExecGetterFunc(func(ctx context.Context, namespace, podName, container, filePath string) ([]byte, error) {
			return []byte(reportJSON), nil
		}),
	}

	alerts, err := r.collectAlertsFromPodReport(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alerts.Total != 3 {
		t.Errorf("expected 3 alerts, got %d", alerts.Total)
	}
	if len(alerts.ByPlugin) != 2 {
		t.Errorf("expected 2 unique plugin alerts, got %d", len(alerts.ByPlugin))
	}
}

func TestCollectAlertsFromPodReport_ReadFileError(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns1"},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "ns1",
			Labels:    map[string]string{"job-name": "test-job"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
		execGetter: podExecGetterFunc(func(ctx context.Context, namespace, podName, container, filePath string) ([]byte, error) {
			return nil, errors.New("file not found")
		}),
	}

	_, err := r.collectAlertsFromPodReport(ctx, job)
	if err == nil {
		t.Error("expected error when readFileFromPod fails")
	}
}

func TestCollectAlertsFromPodReport_InvalidJSON(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns1"},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "ns1",
			Labels:    map[string]string{"job-name": "test-job"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
		execGetter: podExecGetterFunc(func(ctx context.Context, namespace, podName, container, filePath string) ([]byte, error) {
			return []byte("invalid json"), nil
		}),
	}

	_, err := r.collectAlertsFromPodReport(ctx, job)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestCollectAlertsFromPodReport_MultipleSites(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns1"},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "ns1",
			Labels:    map[string]string{"job-name": "test-job"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	// JSON report with multiple sites
	reportJSON := `{
		"site": [
			{
				"alerts": [
					{"pluginid": "10001", "riskcode": "0"},
					{"pluginid": "10002", "riskcode": "1"}
				]
			},
			{
				"alerts": [
					{"pluginid": "10003", "riskcode": "2"},
					{"pluginid": "10004", "riskcode": "3"}
				]
			}
		]
	}`

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
		execGetter: podExecGetterFunc(func(ctx context.Context, namespace, podName, container, filePath string) ([]byte, error) {
			return []byte(reportJSON), nil
		}),
	}

	alerts, err := r.collectAlertsFromPodReport(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alerts.Total != 4 {
		t.Errorf("expected 4 alerts across multiple sites, got %d", alerts.Total)
	}
}

func TestCollectAlertsFromPodReport_ListPodsError(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	// Don't add corev1 types to scheme to trigger list error
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

	_, err := r.collectAlertsFromPodReport(ctx, job)
	if err == nil {
		t.Error("expected error when listing pods fails")
	}
}
