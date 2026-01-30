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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestNormalizeRisk(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"0", "informational"},
		{"1", "low"},
		{"2", "medium"},
		{"3", "high"},
		{" 1 ", "low"},
		{"unknown", "unknown"},
	}

	for _, tc := range cases {
		if got := normalizeRisk(tc.in); got != tc.want {
			t.Fatalf("normalizeRisk(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestScanJobNameWithTimestamp(t *testing.T) {
	ts := time.Unix(1700000000, 0)

	name1 := scanJobNameWithTimestamp("my-scan", ts)
	name2 := scanJobNameWithTimestamp("my-scan", ts)

	// Same input should produce same output
	if name1 != name2 {
		t.Errorf("expected consistent naming, got %q and %q", name1, name2)
	}

	// Should start with zap-scan-
	if name1[:9] != "zap-scan-" {
		t.Errorf("expected name to start with 'zap-scan-', got %q", name1)
	}

	// Should contain the timestamp
	if !containsSubstring(name1, "1700000000") {
		t.Errorf("expected name to contain timestamp, got %q", name1)
	}

	// Different scan names should produce different job names
	name3 := scanJobNameWithTimestamp("other-scan", ts)
	if name1 == name3 {
		t.Errorf("different scan names should produce different job names")
	}

	// Different timestamps should produce different job names
	ts2 := time.Unix(1700001000, 0)
	name4 := scanJobNameWithTimestamp("my-scan", ts2)
	if name1 == name4 {
		t.Errorf("different timestamps should produce different job names")
	}
}

func TestJobNamespaceFor(t *testing.T) {
	// When jobNamespace is nil, use scanNS
	ns := jobNamespaceFor("default", nil)
	if ns != "default" {
		t.Errorf("expected 'default', got %q", ns)
	}

	// When jobNamespace is empty string pointer, use scanNS
	empty := ""
	ns = jobNamespaceFor("default", &empty)
	if ns != "default" {
		t.Errorf("expected 'default', got %q", ns)
	}

	// When jobNamespace is set, use it
	custom := "custom-ns"
	ns = jobNamespaceFor("default", &custom)
	if ns != "custom-ns" {
		t.Errorf("expected 'custom-ns', got %q", ns)
	}
}

func TestJoinShell(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"cmd"}, "cmd"},
		{[]string{"cmd", "arg1", "arg2"}, "cmd arg1 arg2"},
		{[]string{"cmd", "arg with spaces"}, "cmd 'arg with spaces'"},
		{[]string{"cmd", "arg\twith\ttabs"}, "cmd 'arg\twith\ttabs'"},
		{[]string{"cmd", "arg\nwith\nnewlines"}, "cmd 'arg\nwith\nnewlines'"},
		{[]string{}, ""},
	}

	for _, tc := range cases {
		got := joinShell(tc.args)
		if got != tc.want {
			t.Errorf("joinShell(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestIsJobComplete(t *testing.T) {
	cases := []struct {
		name         string
		job          *batchv1.Job
		wantComplete bool
		wantSuccess  bool
	}{
		{
			name:         "no conditions",
			job:          &batchv1.Job{},
			wantComplete: false,
			wantSuccess:  false,
		},
		{
			name: "job complete",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
					},
				},
			},
			wantComplete: true,
			wantSuccess:  true,
		},
		{
			name: "job failed",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
					},
				},
			},
			wantComplete: true,
			wantSuccess:  false,
		},
		{
			name: "condition false",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobComplete, Status: corev1.ConditionFalse},
					},
				},
			},
			wantComplete: false,
			wantSuccess:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			complete, success := isJobComplete(tc.job)
			if complete != tc.wantComplete {
				t.Errorf("complete = %v, want %v", complete, tc.wantComplete)
			}
			if success != tc.wantSuccess {
				t.Errorf("success = %v, want %v", success, tc.wantSuccess)
			}
		})
	}
}

func TestJobFinishedTime(t *testing.T) {
	now := metav1.Now()

	cases := []struct {
		name    string
		job     *batchv1.Job
		wantNil bool
	}{
		{
			name:    "no completion time or conditions",
			job:     &batchv1.Job{},
			wantNil: true,
		},
		{
			name: "has completion time",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					CompletionTime: &now,
				},
			},
			wantNil: false,
		},
		{
			name: "has condition with transition time",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:               batchv1.JobComplete,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: now,
						},
					},
				},
			},
			wantNil: false,
		},
		{
			name: "has failed condition with transition time",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:               batchv1.JobFailed,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: now,
						},
					},
				},
			},
			wantNil: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := jobFinishedTime(tc.job)
			if tc.wantNil && result != nil {
				t.Errorf("expected nil, got %v", result)
			}
			if !tc.wantNil && result == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestBuildZapFullScanJob(t *testing.T) {
	job := buildZapFullScanJob("test-job", "test-ns", "my-scan", "https://example.com", nil, nil, nil, nil)

	// Check basic properties
	if job.Name != "test-job" {
		t.Errorf("expected job name 'test-job', got %q", job.Name)
	}
	if job.Namespace != "test-ns" {
		t.Errorf("expected namespace 'test-ns', got %q", job.Namespace)
	}

	// Check labels
	if job.Labels["spaceship.com/scan-name"] != "my-scan" {
		t.Errorf("expected scan-name label 'my-scan', got %q", job.Labels["spaceship.com/scan-name"])
	}

	// Check default image
	if job.Spec.Template.Spec.Containers[0].Image != "ghcr.io/zaproxy/zaproxy:stable" {
		t.Errorf("expected default image, got %q", job.Spec.Template.Spec.Containers[0].Image)
	}

	// Check containers count
	if len(job.Spec.Template.Spec.Containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(job.Spec.Template.Spec.Containers))
	}

	// Test with custom image
	customImg := "my-registry/zap:custom"
	job = buildZapFullScanJob("test-job", "test-ns", "my-scan", "https://example.com", nil, &customImg, nil, nil)
	if job.Spec.Template.Spec.Containers[0].Image != customImg {
		t.Errorf("expected custom image %q, got %q", customImg, job.Spec.Template.Spec.Containers[0].Image)
	}

	// Test with OpenAPI spec
	openapi := "https://example.com/openapi.json"
	job = buildZapFullScanJob("test-job", "test-ns", "my-scan", "https://example.com", &openapi, nil, nil, nil)
	args := job.Spec.Template.Spec.Containers[0].Args[0]
	if !containsSubstring(args, "-O") || !containsSubstring(args, openapi) {
		t.Errorf("expected OpenAPI flag in args, got %q", args)
	}

	// Test with service account
	saName := "my-sa"
	job = buildZapFullScanJob("test-job", "test-ns", "my-scan", "https://example.com", nil, nil, nil, &saName)
	if job.Spec.Template.Spec.ServiceAccountName != saName {
		t.Errorf("expected service account %q, got %q", saName, job.Spec.Template.Spec.ServiceAccountName)
	}

	// Test with extra args
	extraArgs := []string{"-a", "--custom-flag"}
	job = buildZapFullScanJob("test-job", "test-ns", "my-scan", "https://example.com", nil, nil, extraArgs, nil)
	args = job.Spec.Template.Spec.Containers[0].Args[0]
	if !containsSubstring(args, "-a") || !containsSubstring(args, "--custom-flag") {
		t.Errorf("expected extra args in command, got %q", args)
	}
}

func TestPtr(t *testing.T) {
	// Test with int
	intVal := 42
	intPtr := ptr(intVal)
	if *intPtr != intVal {
		t.Errorf("expected %d, got %d", intVal, *intPtr)
	}

	// Test with string
	strVal := "hello"
	strPtr := ptr(strVal)
	if *strPtr != strVal {
		t.Errorf("expected %q, got %q", strVal, *strPtr)
	}

	// Test with bool
	boolVal := true
	boolPtr := ptr(boolVal)
	if *boolPtr != boolVal {
		t.Errorf("expected %v, got %v", boolVal, *boolPtr)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDeleteJobIfRequested(t *testing.T) {
	ctx := context.Background()

	// Test with nil cleanup - should return nil
	err := deleteJobIfRequested(ctx, nil, &batchv1.Job{}, nil)
	if err != nil {
		t.Errorf("expected nil error for nil cleanup, got %v", err)
	}

	// Test with false cleanup - should return nil
	cleanup := false
	err = deleteJobIfRequested(ctx, nil, &batchv1.Job{}, &cleanup)
	if err != nil {
		t.Errorf("expected nil error for false cleanup, got %v", err)
	}
}

func TestJobFailedReason(t *testing.T) {
	cases := []struct {
		name string
		job  *batchv1.Job
		want string
	}{
		{
			name: "no conditions",
			job:  &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "test-job"}},
			want: "job test-job failed",
		},
		{
			name: "failed with message",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-job"},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:    batchv1.JobFailed,
							Status:  corev1.ConditionTrue,
							Message: "BackoffLimitExceeded",
						},
					},
				},
			},
			want: "BackoffLimitExceeded",
		},
		{
			name: "failed with reason only",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-job"},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
							Reason: "DeadlineExceeded",
						},
					},
				},
			},
			want: "DeadlineExceeded",
		},
		{
			name: "failed condition false",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-job"},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			want: "job test-job failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := jobFailedReason(tc.job)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJobKey(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "test-ns",
		},
	}

	key := jobKey(job)
	if key.Name != "test-job" {
		t.Errorf("expected name 'test-job', got %q", key.Name)
	}
	if key.Namespace != "test-ns" {
		t.Errorf("expected namespace 'test-ns', got %q", key.Namespace)
	}
}

func TestFormatNN(t *testing.T) {
	cases := []struct {
		name string
		nn   types.NamespacedName
		want string
	}{
		{
			name: "with namespace",
			nn:   types.NamespacedName{Name: "my-resource", Namespace: "my-ns"},
			want: "my-ns/my-resource",
		},
		{
			name: "without namespace",
			nn:   types.NamespacedName{Name: "my-resource"},
			want: "my-resource",
		},
		{
			name: "empty",
			nn:   types.NamespacedName{},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatNN(tc.nn)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetJob(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	existingJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-job", Namespace: "ns1"},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(existingJob).Build()

	// Test getting existing job
	job, err := getJob(ctx, c, types.NamespacedName{Name: "existing-job", Namespace: "ns1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if job.Name != "existing-job" {
		t.Errorf("expected job name 'existing-job', got %q", job.Name)
	}

	// Test getting non-existing job
	_, err = getJob(ctx, c, types.NamespacedName{Name: "missing-job", Namespace: "ns1"})
	if err == nil {
		t.Error("expected error for missing job")
	}
}

func TestEnsureJob(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	existingJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-job", Namespace: "ns1"},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(existingJob).Build()

	// Test ensuring existing job - should do nothing
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-job", Namespace: "ns1"},
	}
	err := ensureJob(ctx, c, job)
	if err != nil {
		t.Fatalf("expected no error for existing job, got %v", err)
	}

	// Test ensuring new job - should create it
	newJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "new-job", Namespace: "ns1"},
	}
	err = ensureJob(ctx, c, newJob)
	if err != nil {
		t.Fatalf("expected no error for new job, got %v", err)
	}

	// Verify it was created
	var created batchv1.Job
	err = c.Get(ctx, types.NamespacedName{Name: "new-job", Namespace: "ns1"}, &created)
	if err != nil {
		t.Fatalf("expected job to be created: %v", err)
	}
}

func TestEnsureJob_GetError(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "job1", Namespace: "ns1"},
	}

	// Client that fails on Get with an error other than NotFound
	cl := fake.NewClientBuilder().WithScheme(s).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			return errors.New("simulated get error")
		},
	}).Build()

	err := ensureJob(ctx, cl, job)
	if err == nil {
		t.Error("expected error when Get fails")
	}
	if err.Error() != "simulated get error" {
		t.Errorf("expected 'simulated get error', got %v", err)
	}
}

func TestDeleteJobIfRequested_WithClient(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns1"},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(job).Build()

	// Test with cleanup = true - should delete
	cleanup := true
	err := deleteJobIfRequested(ctx, c, job, &cleanup)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify it was deleted
	var deleted batchv1.Job
	err = c.Get(ctx, types.NamespacedName{Name: "test-job", Namespace: "ns1"}, &deleted)
	if err == nil {
		t.Error("expected job to be deleted")
	}
}
