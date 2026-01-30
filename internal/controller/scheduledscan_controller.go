package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	zapv1alpha1 "github.com/NCCloud/zap-operator/api/v1alpha1"
)

type ZapScheduledScanReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ZapScheduledScanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("scheduledscan", req.NamespacedName)

	var sched zapv1alpha1.ZapScheduledScan
	if err := r.Get(ctx, req.NamespacedName, &sched); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if sched.Spec.Suspend != nil && *sched.Spec.Suspend {
		return ctrl.Result{}, nil
	}

	schedule, err := cron.ParseStandard(sched.Spec.Schedule)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("invalid schedule: %w", err)
	}

	var last time.Time
	if sched.Status.LastScheduleTime != nil {
		last = sched.Status.LastScheduleTime.Time
	}

	now := time.Now()
	next := schedule.Next(last)
	if next.After(now) {
		return ctrl.Result{RequeueAfter: next.Sub(now)}, nil
	}

	// If we are behind, only create one Scan per reconcile.
	policy := "Allow"
	if sched.Spec.ConcurrencyPolicy != nil && *sched.Spec.ConcurrencyPolicy != "" {
		policy = *sched.Spec.ConcurrencyPolicy
	}

	active, err := r.activeScans(ctx, &sched)
	if err != nil {
		return ctrl.Result{}, err
	}

	if policy == "Forbid" && len(active) > 0 {
		log.Info("skipping schedule due to active scan", "active", len(active))
		// Just update lastScheduleTime so we don't spam.
		t := metav1.NewTime(now)
		sched.Status.LastScheduleTime = &t
		_ = r.Status().Update(ctx, &sched)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	if policy == "Replace" && len(active) > 0 {
		for _, s := range active {
			_ = r.Delete(ctx, &s)
		}
	}

	child := &zapv1alpha1.ZapScan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", sched.Name, now.Unix()),
			Namespace: sched.Namespace,
			Labels: map[string]string{
				"spaceship.com/zapscheduledscan": sched.Name,
			},
		},
		Spec: sched.Spec.Template,
	}
	if err := controllerutil.SetControllerReference(&sched, child, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, child); err != nil {
		if errors.IsAlreadyExists(err) {
			return ctrl.Result{RequeueAfter: time.Second * 5}, nil
		}
		return ctrl.Result{}, err
	}

	t := metav1.NewTime(now)
	sched.Status.LastScheduleTime = &t
	_ = r.Status().Update(ctx, &sched)

	log.Info("created scan from schedule", "scan", types.NamespacedName{Name: child.Name, Namespace: child.Namespace})
	return ctrl.Result{RequeueAfter: time.Second * 5}, nil
}

func (r *ZapScheduledScanReconciler) activeScans(ctx context.Context, sched *zapv1alpha1.ZapScheduledScan) ([]zapv1alpha1.ZapScan, error) {
	var scans zapv1alpha1.ZapScanList
	if err := r.List(ctx, &scans, &client.ListOptions{Namespace: sched.Namespace}); err != nil {
		return nil, err
	}

	out := make([]zapv1alpha1.ZapScan, 0)
	for _, s := range scans.Items {
		if metav1.IsControlledBy(&s, sched) {
			if s.Status.Phase == "Running" || s.Status.Phase == "" {
				out = append(out, s)
			}
		}
	}
	return out, nil
}

func (r *ZapScheduledScanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zapv1alpha1.ZapScheduledScan{}).
		Owns(&zapv1alpha1.ZapScan{}).
		Complete(r)
}
