package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zapv1alpha1 "github.com/NCCloud/zap-operator/api/v1alpha1"
)

func TestZapScheduledScanReconciler_SuspendDoesNothing(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	suspend := true
	sched := &zapv1alpha1.ZapScheduledScan{
		ObjectMeta: metav1.ObjectMeta{Name: "ss1", Namespace: "ns1"},
		Spec: zapv1alpha1.ZapScheduledScanSpec{
			Schedule: "* * * * *",
			Suspend:  &suspend,
			Template: zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		},
	}

	r := &ZapScheduledScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScheduledScan{}).WithObjects(sched).Build(),
		Scheme: s,
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: sched.Name, Namespace: sched.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var scans zapv1alpha1.ZapScanList
	if err := r.List(ctx, &scans, &client.ListOptions{Namespace: sched.Namespace}); err != nil {
		t.Fatalf("list scans: %v", err)
	}
	if len(scans.Items) != 0 {
		t.Fatalf("expected no scans created, got %d", len(scans.Items))
	}
}

func TestZapScheduledScanReconciler_InvalidScheduleReturnsError(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	sched := &zapv1alpha1.ZapScheduledScan{
		ObjectMeta: metav1.ObjectMeta{Name: "ss1", Namespace: "ns1"},
		Spec: zapv1alpha1.ZapScheduledScanSpec{
			Schedule: "not a cron",
			Template: zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		},
	}

	r := &ZapScheduledScanReconciler{Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScheduledScan{}).WithObjects(sched).Build(), Scheme: s}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: sched.Name, Namespace: sched.Namespace}})
	if err == nil {
		t.Fatalf("expected error for invalid schedule")
	}
}

func TestZapScheduledScanReconciler_CreatesScanWhenDue(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	// Force due on first reconcile by setting lastScheduleTime far in the past.
	past := metav1.NewTime(time.Unix(0, 0))
	policy := "Allow"
	sched := &zapv1alpha1.ZapScheduledScan{
		ObjectMeta: metav1.ObjectMeta{Name: "ss1", Namespace: "ns1"},
		Spec: zapv1alpha1.ZapScheduledScanSpec{
			Schedule:          "* * * * *",
			ConcurrencyPolicy: &policy,
			Template:          zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		},
		Status: zapv1alpha1.ZapScheduledScanStatus{LastScheduleTime: &past},
	}

	r := &ZapScheduledScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScheduledScan{}).WithObjects(sched).Build(),
		Scheme: s,
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: sched.Name, Namespace: sched.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var scans zapv1alpha1.ZapScanList
	if err := r.List(ctx, &scans, &client.ListOptions{Namespace: sched.Namespace}); err != nil {
		t.Fatalf("list scans: %v", err)
	}
	if len(scans.Items) != 1 {
		t.Fatalf("expected 1 scan created, got %d", len(scans.Items))
	}
	if scans.Items[0].Labels["spaceship.com/zapscheduledscan"] != sched.Name {
		t.Fatalf("expected scheduled scan label to be set")
	}
}

func TestZapScheduledScanReconciler_ForbidPolicySkipsIfActive(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	past := metav1.NewTime(time.Unix(0, 0))
	policy := "Forbid"
	sched := &zapv1alpha1.ZapScheduledScan{
		ObjectMeta: metav1.ObjectMeta{Name: "ss1", Namespace: "ns1", UID: "sched-uid"},
		Spec: zapv1alpha1.ZapScheduledScanSpec{
			Schedule:          "* * * * *",
			ConcurrencyPolicy: &policy,
			Template:          zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		},
		Status: zapv1alpha1.ZapScheduledScanStatus{LastScheduleTime: &past},
	}

	// Create an active scan owned by this scheduled scan
	activeScan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-scan",
			Namespace: "ns1",
			Labels:    map[string]string{"spaceship.com/zapscheduledscan": sched.Name},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "spaceship.com/v1alpha1",
					Kind:       "ZapScheduledScan",
					Name:       sched.Name,
					UID:        sched.UID,
					Controller: ptr(true),
				},
			},
		},
		Spec:   zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status: zapv1alpha1.ZapScanStatus{Phase: "Running"},
	}

	r := &ZapScheduledScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScheduledScan{}).WithObjects(sched, activeScan).Build(),
		Scheme: s,
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: sched.Name, Namespace: sched.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Should still have only 1 scan (the active one, no new one created)
	var scans zapv1alpha1.ZapScanList
	if err := r.List(ctx, &scans, &client.ListOptions{Namespace: sched.Namespace}); err != nil {
		t.Fatalf("list scans: %v", err)
	}
	if len(scans.Items) != 1 {
		t.Fatalf("expected 1 scan (forbid policy should skip), got %d", len(scans.Items))
	}
}

func TestZapScheduledScanReconciler_ReplacePolicyDeletesActive(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	past := metav1.NewTime(time.Unix(0, 0))
	policy := "Replace"
	sched := &zapv1alpha1.ZapScheduledScan{
		ObjectMeta: metav1.ObjectMeta{Name: "ss1", Namespace: "ns1", UID: "sched-uid"},
		Spec: zapv1alpha1.ZapScheduledScanSpec{
			Schedule:          "* * * * *",
			ConcurrencyPolicy: &policy,
			Template:          zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		},
		Status: zapv1alpha1.ZapScheduledScanStatus{LastScheduleTime: &past},
	}

	// Create an active scan owned by this scheduled scan
	activeScan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-scan",
			Namespace: "ns1",
			Labels:    map[string]string{"spaceship.com/zapscheduledscan": sched.Name},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "spaceship.com/v1alpha1",
					Kind:       "ZapScheduledScan",
					Name:       sched.Name,
					UID:        sched.UID,
					Controller: ptr(true),
				},
			},
		},
		Spec:   zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status: zapv1alpha1.ZapScanStatus{Phase: "Running"},
	}

	r := &ZapScheduledScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScheduledScan{}).WithObjects(sched, activeScan).Build(),
		Scheme: s,
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: sched.Name, Namespace: sched.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Replace policy should delete the active scan and create a new one
	var scans zapv1alpha1.ZapScanList
	if err := r.List(ctx, &scans, &client.ListOptions{Namespace: sched.Namespace}); err != nil {
		t.Fatalf("list scans: %v", err)
	}
	// The old scan should be deleted (or marked for deletion) and a new one created
	// With fake client, delete is immediate
	if len(scans.Items) != 1 {
		t.Fatalf("expected 1 scan after replace, got %d", len(scans.Items))
	}
	// The scan should be the new one, not the old "active-scan"
	if scans.Items[0].Name == "active-scan" {
		t.Error("expected old scan to be replaced")
	}
}

func TestZapScheduledScanReconciler_NotFoundIgnored(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	r := &ZapScheduledScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).Build(),
		Scheme: s,
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: "missing", Namespace: "ns"}})
	if err != nil {
		t.Fatalf("expected not found to be ignored, got: %v", err)
	}
}

func TestZapScheduledScanReconciler_NotDueYet(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	// Set lastScheduleTime to now, so next run is in the future
	now := metav1.Now()
	sched := &zapv1alpha1.ZapScheduledScan{
		ObjectMeta: metav1.ObjectMeta{Name: "ss1", Namespace: "ns1"},
		Spec: zapv1alpha1.ZapScheduledScanSpec{
			Schedule: "0 0 * * *", // Once a day at midnight
			Template: zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		},
		Status: zapv1alpha1.ZapScheduledScanStatus{LastScheduleTime: &now},
	}

	r := &ZapScheduledScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScheduledScan{}).WithObjects(sched).Build(),
		Scheme: s,
	}

	result, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: sched.Name, Namespace: sched.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Should requeue for when schedule is due
	if result.RequeueAfter == 0 {
		t.Error("expected requeue for future schedule")
	}

	// No scan should be created
	var scans zapv1alpha1.ZapScanList
	if err := r.List(ctx, &scans, &client.ListOptions{Namespace: sched.Namespace}); err != nil {
		t.Fatalf("list scans: %v", err)
	}
	if len(scans.Items) != 0 {
		t.Fatalf("expected no scans created, got %d", len(scans.Items))
	}
}

func TestZapScheduledScanReconciler_DefaultConcurrencyPolicy(t *testing.T) {
	ctx := context.Background()

	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := zapv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add zap scheme: %v", err)
	}

	past := metav1.NewTime(time.Unix(0, 0))
	// No concurrency policy set - should default to Allow
	sched := &zapv1alpha1.ZapScheduledScan{
		ObjectMeta: metav1.ObjectMeta{Name: "ss1", Namespace: "ns1"},
		Spec: zapv1alpha1.ZapScheduledScanSpec{
			Schedule: "* * * * *",
			Template: zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		},
		Status: zapv1alpha1.ZapScheduledScanStatus{LastScheduleTime: &past},
	}

	r := &ZapScheduledScanReconciler{
		Client: fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScheduledScan{}).WithObjects(sched).Build(),
		Scheme: s,
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: sched.Name, Namespace: sched.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var scans zapv1alpha1.ZapScanList
	if err := r.List(ctx, &scans, &client.ListOptions{Namespace: sched.Namespace}); err != nil {
		t.Fatalf("list scans: %v", err)
	}
	if len(scans.Items) != 1 {
		t.Fatalf("expected 1 scan created with default policy, got %d", len(scans.Items))
	}
}
