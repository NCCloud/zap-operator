package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zapv1alpha1 "github.com/NCCloud/zap-operator/api/v1alpha1"
)

func TestScanReconciler_ParsesAlertsFromReporterLogs(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	creationTime := metav1.NewTime(time.Unix(1700000000, 0))
	jobName := scanJobNameWithTimestamp("s1", creationTime.Time)

	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "Running", JobName: jobName},
	}

	finished := metav1.Now()
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: scan.Status.JobName, Namespace: scan.Namespace},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: finished}},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p1",
			Namespace: scan.Namespace,
			Labels:    map[string]string{"job-name": job.Name},
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	logText := "random noise\n" +
		"zap-operator: begin zap.json\n" +
		"{\"site\":[{\"alerts\":[" +
		"{\"pluginid\":\"100\",\"riskcode\":\"3\"}," +
		"{\"pluginid\":\"100\",\"riskcode\":\"3\"}," +
		"{\"pluginid\":\"200\",\"riskcode\":\"1\"}" +
		"]}]}\n" +
		"zap-operator: end zap.json\n"

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan, job, pod).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			if container != "reporter" {
				t.Fatalf("expected reporter container, got %q", container)
			}
			return []byte(logText), nil
		}),
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var updated zapv1alpha1.ZapScan
	if err := r.Get(ctx, types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}, &updated); err != nil {
		t.Fatalf("get scan: %v", err)
	}
	if updated.Status.AlertsFound != 3 {
		t.Fatalf("expected alertsFound=3, got %d", updated.Status.AlertsFound)
	}
	if updated.Status.Phase != "Succeeded" {
		t.Fatalf("expected phase=Succeeded, got %q", updated.Status.Phase)
	}
}

func TestCollectAlertsFromJobLogs_NoPods(t *testing.T) {
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

	alerts, err := r.collectAlertsFromJobLogs(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alerts.Total != 0 {
		t.Errorf("expected 0 alerts for no pods, got %d", alerts.Total)
	}
}

func TestCollectAlertsFromJobLogs_LogsGetterError(t *testing.T) {
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
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return nil, errors.New("failed to get logs")
		}),
	}

	_, err := r.collectAlertsFromJobLogs(ctx, job)
	if err == nil {
		t.Error("expected error when logs getter fails")
	}
}

func TestCollectAlertsFromJobLogs_NoMarker(t *testing.T) {
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
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			// No zap-operator marker in logs
			return []byte("some random log output without the marker"), nil
		}),
	}

	alerts, err := r.collectAlertsFromJobLogs(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return empty alerts when marker is not found
	if alerts.Total != 0 {
		t.Errorf("expected 0 alerts when marker not found, got %d", alerts.Total)
	}
}

func TestCollectAlertsFromJobLogs_NoJSONAfterMarker(t *testing.T) {
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
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			// Has marker but no JSON brace after it
			return []byte("zap-operator: begin zap.json\nno json here"), nil
		}),
	}

	alerts, err := r.collectAlertsFromJobLogs(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return empty alerts when no JSON found
	if alerts.Total != 0 {
		t.Errorf("expected 0 alerts when no JSON found, got %d", alerts.Total)
	}
}

func TestCollectAlertsFromJobLogs_InvalidJSON(t *testing.T) {
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
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			// Has marker with invalid JSON
			return []byte("zap-operator: begin zap.json\n{invalid json}"), nil
		}),
	}

	alerts, err := r.collectAlertsFromJobLogs(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error (invalid JSON should be non-fatal): %v", err)
	}
	// Should return empty alerts when JSON is invalid
	if alerts.Total != 0 {
		t.Errorf("expected 0 alerts for invalid JSON, got %d", alerts.Total)
	}
}

func TestCollectAlertsFromJobLogs_MultiplePods(t *testing.T) {
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
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-2",
			Namespace: "ns1",
			Labels:    map[string]string{"job-name": "test-job"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	logText := "zap-operator: begin zap.json\n" +
		"{\"site\":[{\"alerts\":[{\"pluginid\":\"100\",\"riskcode\":\"2\"}]}]}\n"

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod1, pod2).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return []byte(logText), nil
		}),
	}

	alerts, err := r.collectAlertsFromJobLogs(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should aggregate alerts from multiple pods
	if alerts.Total != 2 {
		t.Errorf("expected 2 alerts from 2 pods, got %d", alerts.Total)
	}
}

func TestCollectAlertsFromJobLogs_AllRiskLevels(t *testing.T) {
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
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	logText := "zap-operator: begin zap.json\n" +
		`{"site":[{"alerts":[
			{"pluginid":"1","riskcode":"0"},
			{"pluginid":"2","riskcode":"1"},
			{"pluginid":"3","riskcode":"2"},
			{"pluginid":"4","riskcode":"3"}
		]}]}`

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithObjects(job, pod).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return []byte(logText), nil
		}),
	}

	alerts, err := r.collectAlertsFromJobLogs(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alerts.Total != 4 {
		t.Errorf("expected 4 alerts, got %d", alerts.Total)
	}
	if len(alerts.ByPlugin) != 4 {
		t.Errorf("expected 4 plugin alerts, got %d", len(alerts.ByPlugin))
	}

	// Verify risk levels are normalized
	riskMap := make(map[string]bool)
	for _, a := range alerts.ByPlugin {
		riskMap[a.Risk] = true
	}
	expectedRisks := []string{"informational", "low", "medium", "high"}
	for _, risk := range expectedRisks {
		if !riskMap[risk] {
			t.Errorf("expected risk level %q not found", risk)
		}
	}
}
