package controller

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zapv1alpha1 "github.com/NCCloud/zap-operator/api/v1alpha1"
)

func TestScanReconciler_CreatesJobAndSetsStatusRunning(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return nil, nil
		}),
	}

	creationTime := metav1.NewTime(time.Unix(1700000000, 0))
	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
	}
	if err := r.Create(ctx, scan); err != nil {
		t.Fatalf("create scan: %v", err)
	}

	// Re-fetch to get the actual creation timestamp set by the fake client
	if err := r.Get(ctx, types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}, scan); err != nil {
		t.Fatalf("get scan: %v", err)
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Get the updated scan to find the job name
	var updated zapv1alpha1.ZapScan
	if err := r.Get(ctx, types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}, &updated); err != nil {
		t.Fatalf("get scan: %v", err)
	}
	if updated.Status.Phase != "Running" {
		t.Fatalf("expected status.phase=Running, got %q", updated.Status.Phase)
	}
	if updated.Status.JobName == "" {
		t.Fatalf("expected status.jobName to be set")
	}
	if updated.Status.StartedAt == nil {
		t.Fatalf("expected status.startedAt to be set")
	}

	// Verify the job exists with the name from status
	jobNN := types.NamespacedName{Name: updated.Status.JobName, Namespace: scan.Namespace}
	var job batchv1.Job
	if err := r.Get(ctx, jobNN, &job); err != nil {
		t.Fatalf("expected job to be created: %v", err)
	}
}

func TestScanReconciler_JobCompletesSucceeded(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	now := metav1.Now()
	finished := metav1.NewTime(now.Add(5 * time.Second))
	creationTime := metav1.NewTime(time.Unix(1700000000, 0))
	jobName := scanJobNameWithTimestamp("s1", creationTime.Time)

	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "Running", JobName: jobName, StartedAt: &now},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: scan.Status.JobName, Namespace: scan.Namespace},
		Status: batchv1.JobStatus{
			CompletionTime: &finished,
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: finished},
			},
		},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan, job).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return []byte(""), nil
		}),
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err != nil {
		// this reconcile will attempt to parse logs; it must not block completion
		t.Fatalf("reconcile: %v", err)
	}

	var updated zapv1alpha1.ZapScan
	if err := r.Get(ctx, types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}, &updated); err != nil {
		t.Fatalf("get scan: %v", err)
	}
	if updated.Status.Phase != "Succeeded" {
		t.Fatalf("expected status.phase=Succeeded, got %q", updated.Status.Phase)
	}
	if updated.Status.FinishedAt == nil {
		t.Fatalf("expected status.finishedAt to be set")
	}
}

func TestScanReconciler_JobCompletesFailedSetsReason(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	started := metav1.Now()
	finished := metav1.NewTime(started.Add(5 * time.Second))
	creationTime := metav1.NewTime(time.Unix(1700000000, 0))
	jobName := scanJobNameWithTimestamp("s1", creationTime.Time)

	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "Running", JobName: jobName, StartedAt: &started},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: scan.Status.JobName, Namespace: scan.Namespace},
		Status: batchv1.JobStatus{
			CompletionTime: &finished,
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "CrashLoop", LastTransitionTime: finished},
			},
		},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan, job).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return []byte(""), nil
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
	if updated.Status.Phase != "Failed" {
		t.Fatalf("expected status.phase=Failed, got %q", updated.Status.Phase)
	}
	if updated.Status.LastError == "" {
		t.Fatalf("expected status.lastError to be set")
	}
}

func TestScanReconciler_IgnoreNotFoundScan(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	r := &ScanReconciler{Client: fake.NewClientBuilder().WithScheme(s).Build(), Scheme: s}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: "missing", Namespace: "ns"}})
	if err != nil {
		if !errors.IsNotFound(err) {
			t.Fatalf("expected not found to be ignored, got: %v", err)
		}
	}
}

func TestScanReconciler_AlreadyCompletedScan(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	// Test with Succeeded phase
	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "completed-scan", Namespace: "ns1"},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "Succeeded"},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan).Build(),
		Scheme: s,
	}

	result, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Should return empty result for already completed scan
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue for completed scan, got %v", result.RequeueAfter)
	}

	// Test with Failed phase
	scanFailed := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "failed-scan", Namespace: "ns1"},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "Failed"},
	}

	r2 := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scanFailed).Build(),
		Scheme: s,
	}

	result, err = r2.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scanFailed.Name, Namespace: scanFailed.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue for failed scan, got %v", result.RequeueAfter)
	}
}

func TestScanReconciler_JobInProgress(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	now := metav1.Now()
	creationTime := metav1.NewTime(time.Unix(1700000000, 0))
	jobName := scanJobNameWithTimestamp("s1", creationTime.Time)

	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "Running", JobName: jobName, StartedAt: &now},
	}

	// Job exists but not complete (no conditions)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: scan.Namespace},
		Status:     batchv1.JobStatus{Active: 1},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan, job).Build(),
		Scheme: s,
	}

	result, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Should requeue since job is still running
	if result.RequeueAfter == 0 {
		t.Error("expected requeue for in-progress job")
	}
}

func TestScanReconciler_JobNamespaceHelper(t *testing.T) {
	// Test the jobNamespaceFor helper function behavior
	// Cross-namespace owner references aren't allowed in K8s, but we can test the namespace logic

	// When jobNamespace is nil, use scan's namespace
	ns := jobNamespaceFor("scan-ns", nil)
	if ns != "scan-ns" {
		t.Errorf("expected 'scan-ns', got %q", ns)
	}

	// When jobNamespace is set, use it
	customNS := "custom-ns"
	ns = jobNamespaceFor("scan-ns", &customNS)
	if ns != "custom-ns" {
		t.Errorf("expected 'custom-ns', got %q", ns)
	}

	// When jobNamespace is empty string, use scan's namespace
	emptyNS := ""
	ns = jobNamespaceFor("scan-ns", &emptyNS)
	if ns != "scan-ns" {
		t.Errorf("expected 'scan-ns', got %q", ns)
	}
}

func TestScanReconciler_SkipsNewJobIfAlreadyFinished(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	finished := metav1.Now()
	// Scan has FinishedAt set but no JobName (edge case)
	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1"},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "Running", FinishedAt: &finished},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan).Build(),
		Scheme: s,
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Should not create a new job since scan already has FinishedAt
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs); err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 0 {
		t.Errorf("expected no jobs to be created, got %d", len(jobs.Items))
	}
}

func TestScanReconciler_JobInProgressWithEmptyPhase(t *testing.T) {
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

	// Scan with empty phase (initial state)
	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "", JobName: jobName},
	}

	// Job exists but not complete
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: scan.Namespace},
		Status:     batchv1.JobStatus{Active: 1},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan, job).Build(),
		Scheme: s,
	}

	result, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Should requeue since job is still running
	if result.RequeueAfter == 0 {
		t.Error("expected requeue for in-progress job")
	}

	// Phase should be set to Running
	var updated zapv1alpha1.ZapScan
	if err := r.Get(ctx, types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}, &updated); err != nil {
		t.Fatalf("get scan: %v", err)
	}
	if updated.Status.Phase != "Running" {
		t.Errorf("expected phase to be 'Running', got %q", updated.Status.Phase)
	}
}

func TestScanReconciler_JobFailedWithParseError(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	started := metav1.Now()
	finished := metav1.NewTime(started.Add(5 * time.Second))
	creationTime := metav1.NewTime(time.Unix(1700000000, 0))
	jobName := scanJobNameWithTimestamp("s1", creationTime.Time)

	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "Running", JobName: jobName, StartedAt: &started},
	}

	// Job failed
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: scan.Status.JobName, Namespace: scan.Namespace},
		Status: batchv1.JobStatus{
			CompletionTime: &finished,
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: "BackoffLimitExceeded", LastTransitionTime: finished},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "ns1",
			Labels:    map[string]string{"job-name": jobName},
		},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan, job, pod).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return nil, errors.NewServiceUnavailable("logs not available")
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
	if updated.Status.Phase != "Failed" {
		t.Errorf("expected phase 'Failed', got %q", updated.Status.Phase)
	}
	// LastError should contain the parse error since it happened first
	if updated.Status.LastError == "" {
		t.Error("expected lastError to be set")
	}
}

func TestScanReconciler_JobCompletedWithFinishedAtNil(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	started := metav1.Now()
	creationTime := metav1.NewTime(time.Unix(1700000000, 0))
	jobName := scanJobNameWithTimestamp("s1", creationTime.Time)

	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "Running", JobName: jobName, StartedAt: &started},
	}

	// Job complete but with no CompletionTime or LastTransitionTime (edge case)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: scan.Status.JobName, Namespace: scan.Namespace},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan, job).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return []byte(""), nil
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
	if updated.Status.Phase != "Succeeded" {
		t.Errorf("expected phase 'Succeeded', got %q", updated.Status.Phase)
	}
}

func TestScanReconciler_WithJobNamespace(t *testing.T) {
	// This test verifies that when jobNamespace is set to a different namespace,
	// the controller attempts to create the job there but Kubernetes prevents
	// cross-namespace owner references. The test validates the error is returned.
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	customNS := "custom-job-ns"
	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1"},
		Spec: zapv1alpha1.ZapScanSpec{
			Target:       "https://example.com",
			JobNamespace: &customNS,
		},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return nil, nil
		}),
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	// Cross-namespace owner references are not allowed, so we expect an error
	if err == nil {
		t.Error("expected error for cross-namespace owner reference")
	}
}

func TestScanReconciler_UseExistingJobName(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	existingJobName := "existing-job-name"
	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1"},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{JobName: existingJobName},
	}

	r := &ScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan).Build(),
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return nil, nil
		}),
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Verify job was created with the existing name
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs); err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs.Items))
	}
	if jobs.Items[0].Name != existingJobName {
		t.Errorf("expected job name %q, got %q", existingJobName, jobs.Items[0].Name)
	}
}
