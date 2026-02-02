package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	zapv1alpha1 "github.com/NCCloud/zap-operator/api/v1alpha1"
	"github.com/NCCloud/zap-operator/internal/metrics"
)

type ScanReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// logsGetter allows tests to inject pod log contents.
	// If nil, the reconciler uses its default implementation.
	logsGetter podLogsGetter
}

func (r *ScanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("scan", req.NamespacedName)

	var scan zapv1alpha1.ZapScan
	if err := r.Get(ctx, req.NamespacedName, &scan); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	jobNS := jobNamespaceFor(scan.Namespace, scan.Spec.JobNamespace)

	// If scan already completed, don't do anything
	if scan.Status.Phase == "Succeeded" || scan.Status.Phase == "Failed" {
		log.Info("scan already completed", "phase", scan.Status.Phase)
		return ctrl.Result{}, nil
	}

	// If we have a job name in status, use it; otherwise create a new one with timestamp
	// Use scan's creation timestamp to ensure consistent naming across reconcile retries
	jobName := scan.Status.JobName
	if jobName == "" {
		jobName = scanJobNameWithTimestamp(scan.Name, scan.CreationTimestamp.Time)
	}

	jobNN := types.NamespacedName{Name: jobName, Namespace: jobNS}

	var job batchv1.Job
	err := r.Get(ctx, jobNN, &job)
	if errors.IsNotFound(err) {
		// Don't create a new job if scan already has a FinishedAt (completed previously)
		if scan.Status.FinishedAt != nil {
			log.Info("scan already finished, skipping new job creation")
			return ctrl.Result{}, nil
		}

		newJob := buildZapFullScanJob(jobName, jobNS, scan.Name, scan.Spec.Target, scan.Spec.OpenAPI, scan.Spec.Image, scan.Spec.Args, scan.Spec.ServiceAccountName)
		if err := controllerutil.SetControllerReference(&scan, newJob, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, newJob); err != nil {
			if errors.IsAlreadyExists(err) {
				// Job was created in a previous reconcile but status update failed
				// Just continue to update status
				log.Info("job already exists, updating status", "job", jobName)
			} else {
				return ctrl.Result{}, err
			}
		}

		now := metav1.Now()
		scan.Status.Phase = "Running"
		scan.Status.JobName = newJob.Name
		scan.Status.StartedAt = &now
		scan.Status.FinishedAt = nil
		scan.Status.LastError = ""
		if err := r.Status().Update(ctx, &scan); err != nil {
			return ctrl.Result{}, err
		}

		// Track scan in progress
		metrics.IncScansInProgress(scan.Namespace)

		log.Info("created scan job", "job", jobNN)
		return ctrl.Result{RequeueAfter: defaultPollInterval}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	complete, succeeded := isJobComplete(&job)
	if !complete {
		if scan.Status.Phase == "" || scan.Status.Phase == "Running" {
			scan.Status.Phase = "Running"
			_ = r.Status().Update(ctx, &scan)
		}
		return ctrl.Result{RequeueAfter: defaultPollInterval}, nil
	}

	finishedAt := jobFinishedTime(&job)
	if finishedAt != nil {
		scan.Status.FinishedAt = finishedAt
	}

	// ZAP jobs often exit non-zero when alerts are found.
	// We still want to parse the report and emit metrics in that case.
	alerts, parseErr := r.collectAlertsFromJobLogs(ctx, &job)
	if parseErr != nil {
		log.Error(parseErr, "failed to parse alerts from scan report")
		scan.Status.LastError = parseErr.Error()
	} else {
		scan.Status.AlertsFound = int64(alerts.Total)
	}

	// Calculate scan duration
	var durationSeconds float64
	if scan.Status.StartedAt != nil && finishedAt != nil {
		durationSeconds = finishedAt.Time.Sub(scan.Status.StartedAt.Time).Seconds()
	}

	// Determine final phase
	var finalPhase string
	var finalStatus string
	if succeeded {
		finalPhase = "Succeeded"
		finalStatus = "succeeded"
		scan.Status.LastError = ""
	} else {
		finalPhase = "Failed"
		finalStatus = "failed"
		if scan.Status.LastError == "" {
			scan.Status.LastError = jobFailedReason(&job)
		}
	}
	scan.Status.Phase = finalPhase

	// Update status FIRST, only emit metrics if update succeeds
	if err := r.Status().Update(ctx, &scan); err != nil {
		return ctrl.Result{}, err
	}

	// Now that status is persisted, emit metrics (won't be double-counted on retry)
	if alerts != nil {
		for _, a := range alerts.ByPlugin {
			metrics.IncAlert(scan.Namespace, scan.Spec.Target, a.Risk, a.PluginID, a.Count)
		}
	}
	metrics.IncScanRun(scan.Namespace, scan.Spec.Target, finalStatus)
	metrics.ObserveScanDuration(scan.Namespace, scan.Spec.Target, durationSeconds)
	metrics.SetLastScanTimestamp(scan.Namespace, scan.Spec.Target, finalStatus, float64(time.Now().Unix()))
	metrics.DecScansInProgress(scan.Namespace)

	// Jobs are kept for historical reference (not deleted)
	log.Info("scan completed", "phase", finalPhase, "job", job.Name)

	return ctrl.Result{}, nil
}

const defaultPollInterval = 10 * time.Second

func (r *ScanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	metrics.Register(crmetrics.Registry)

	return ctrl.NewControllerManagedBy(mgr).
		For(&zapv1alpha1.ZapScan{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

type parsedAlerts struct {
	Total    int
	ByPlugin []pluginAlert
}

type pluginAlert struct {
	PluginID string
	Risk     string
	Count    int
}

func (r *ScanReconciler) collectAlertsFromJobLogs(ctx context.Context, job *batchv1.Job) (*parsedAlerts, error) {
	pods, err := r.podsForJob(ctx, job)
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return &parsedAlerts{}, nil
	}

	// We parse zap.json from logs emitted by the "reporter" sidecar.

	all := parsedAlerts{Total: 0}
	acc := map[string]*pluginAlert{}

	for _, p := range pods.Items {
		lg := r.logsGetter
		if lg == nil {
			lg = r
		}
		logBytes, err := lg.getPodLogs(ctx, p.Namespace, p.Name, "reporter")
		if err != nil {
			return nil, err
		}
		// Reporter sidecar prints the full JSON report between markers.
		text := string(logBytes)
		start := strings.Index(text, "zap-operator: begin zap.json")
		if start < 0 {
			continue
		}
		text = text[start:]
		idx := strings.Index(text, "{")
		if idx < 0 {
			continue
		}
		cand := text[idx:]

		var report zapJSONReport
		if err := json.NewDecoder(strings.NewReader(cand)).Decode(&report); err != nil {
			// not fatal; keep going
			continue
		}

		for _, site := range report.Site {
			for _, a := range site.Alerts {
				all.Total++
				pid := a.PluginID
				risk := normalizeRisk(a.RiskCode)
				key := pid + ":" + risk
				pa, ok := acc[key]
				if !ok {
					pa = &pluginAlert{PluginID: pid, Risk: risk}
					acc[key] = pa
				}
				pa.Count++
			}
		}
	}

	for _, v := range acc {
		all.ByPlugin = append(all.ByPlugin, *v)
	}

	return &all, nil
}

type zapJSONReport struct {
	Site []struct {
		Alerts []struct {
			PluginID string `json:"pluginid"`
			RiskCode string `json:"riskcode"`
		} `json:"alerts"`
	} `json:"site"`
}

func normalizeRisk(riskCode string) string {
	switch strings.TrimSpace(riskCode) {
	case "0":
		return "informational"
	case "1":
		return "low"
	case "2":
		return "medium"
	case "3":
		return "high"
	default:
		return riskCode
	}
}

func jobFailedReason(job *batchv1.Job) string {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			if c.Message != "" {
				return c.Message
			}
			if c.Reason != "" {
				return c.Reason
			}
		}
	}
	return fmt.Sprintf("job %s failed", job.Name)
}
