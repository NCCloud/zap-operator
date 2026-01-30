package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	zapv1alpha1 "github.com/NCCloud/zap-operator/api/v1alpha1"
)

func TestScanReconciler_CreateJobError(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = zapv1alpha1.AddToScheme(s)

	creationTime := metav1.NewTime(time.Unix(1700000000, 0))
	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
	}

	// Create a client that fails on Job creation
	cl := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*batchv1.Job); ok {
				return errors.New("simulated create error")
			}
			return client.Create(ctx, obj, opts...)
		},
	}).Build()

	r := &ScanReconciler{
		Client:     cl,
		Scheme:     s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) { return nil, nil }),
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err == nil {
		t.Error("expected error from Reconcile when Job creation fails")
	}
	if err.Error() != "simulated create error" {
		t.Errorf("expected 'simulated create error', got %v", err)
	}
}

func TestScanReconciler_StatusUpdateError(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = zapv1alpha1.AddToScheme(s)

	creationTime := metav1.NewTime(time.Unix(1700000000, 0))
	jobName := scanJobNameWithTimestamp("s1", creationTime.Time)
	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
		Status:     zapv1alpha1.ZapScanStatus{Phase: "Running", JobName: jobName},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: "ns1"},
		Status:     batchv1.JobStatus{Succeeded: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: "True"}}},
	}

	// Create a client that fails on Status update
	cl := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan, job).WithInterceptorFuncs(interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if _, ok := obj.(*zapv1alpha1.ZapScan); ok && subResourceName == "status" {
				return errors.New("simulated status update error")
			}
			return client.Status().Update(ctx, obj, opts...)
		},
	}).Build()

	r := &ScanReconciler{
		Client: cl,
		Scheme: s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) {
			return []byte("{}"), nil
		}),
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err == nil {
		t.Error("expected error from Reconcile when Status update fails")
	}
}

func TestScheduledScanReconciler_ListError(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = zapv1alpha1.AddToScheme(s)

	sched := &zapv1alpha1.ZapScheduledScan{
		ObjectMeta: metav1.ObjectMeta{Name: "sched1", Namespace: "ns1"},
		Spec:       zapv1alpha1.ZapScheduledScanSpec{Schedule: "* * * * *"},
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(sched).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			return errors.New("simulated list error")
		},
	}).Build()

	r := &ZapScheduledScanReconciler{
		Client: cl,
		Scheme: s,
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: sched.Name, Namespace: sched.Namespace}})
	if err == nil {
		t.Error("expected error when List fails")
	}
}

func TestScheduledScanReconciler_CreateScanError(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = zapv1alpha1.AddToScheme(s)

	// Set LastScheduleTime way in the past to trigger creation
	past := metav1.Time{Time: time.Now().Add(-1 * time.Hour)}
	sched := &zapv1alpha1.ZapScheduledScan{
		ObjectMeta: metav1.ObjectMeta{Name: "sched1", Namespace: "ns1"},
		Spec:       zapv1alpha1.ZapScheduledScanSpec{Schedule: "* * * * *"},
		Status:     zapv1alpha1.ZapScheduledScanStatus{LastScheduleTime: &past},
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(sched).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*zapv1alpha1.ZapScan); ok {
				return errors.New("simulated scan create error")
			}
			return client.Create(ctx, obj, opts...)
		},
	}).Build()

	r := &ZapScheduledScanReconciler{
		Client: cl,
		Scheme: s,
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: sched.Name, Namespace: sched.Namespace}})
	if err == nil {
		t.Error("expected error when ZapScan create fails")
	}
}

func TestScanReconciler_CreateJobAlreadyExists(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = zapv1alpha1.AddToScheme(s)

	creationTime := metav1.NewTime(time.Unix(1700000000, 0))
	// Scan with no JobName, should trigger job creation
	scan := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "ns1", CreationTimestamp: creationTime},
		Spec:       zapv1alpha1.ZapScanSpec{Target: "https://example.com"},
	}

	// Create a client that returns AlreadyExists on Job creation
	cl := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&zapv1alpha1.ZapScan{}).WithObjects(scan).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*batchv1.Job); ok {
				return k8serrors.NewAlreadyExists(batchv1.Resource("jobs"), obj.GetName())
			}
			return client.Create(ctx, obj, opts...)
		},
	}).Build()

	r := &ScanReconciler{
		Client:     cl,
		Scheme:     s,
		logsGetter: podLogsGetterFunc(func(ctx context.Context, namespace, podName, container string) ([]byte, error) { return nil, nil }),
	}

	_, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}})
	if err != nil {
		t.Errorf("expected no error for AlreadyExists, got %v", err)
	}

	// Verify status was updated to Running
	var updated zapv1alpha1.ZapScan
	if err := cl.Get(ctx, types.NamespacedName{Name: scan.Name, Namespace: scan.Namespace}, &updated); err != nil {
		t.Fatalf("failed to get scan: %v", err)
	}
	if updated.Status.Phase != "Running" {
		t.Errorf("expected phase Running, got %q", updated.Status.Phase)
	}
}

func TestScheduledScanReconciler_CreateScanAlreadyExists(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = zapv1alpha1.AddToScheme(s)

	past := metav1.Time{Time: time.Now().Add(-1 * time.Hour)}
	sched := &zapv1alpha1.ZapScheduledScan{
		ObjectMeta: metav1.ObjectMeta{Name: "sched1", Namespace: "ns1"},
		Spec:       zapv1alpha1.ZapScheduledScanSpec{Schedule: "* * * * *"},
		Status:     zapv1alpha1.ZapScheduledScanStatus{LastScheduleTime: &past},
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(sched).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*zapv1alpha1.ZapScan); ok {
				return k8serrors.NewAlreadyExists(zapv1alpha1.GroupVersion.WithResource("zapscans").GroupResource(), obj.GetName())
			}
			return client.Create(ctx, obj, opts...)
		},
	}).Build()

	r := &ZapScheduledScanReconciler{
		Client: cl,
		Scheme: s,
	}

	// Should successfully requeue
	res, err := r.Reconcile(ctrl.LoggerInto(ctx, ctrl.Log.WithName("test")), ctrl.Request{NamespacedName: types.NamespacedName{Name: sched.Name, Namespace: sched.Namespace}})
	if err != nil {
		t.Errorf("expected no error for AlreadyExists, got %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected RequeueAfter set")
	}
}
