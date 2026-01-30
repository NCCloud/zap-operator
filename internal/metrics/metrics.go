package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	alertsFound = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zap_operator_alerts_found_total",
			Help: "Total number of ZAP alerts found by zap-full-scan jobs.",
		},
		[]string{"scan_target", "scan_namespace", "risk", "plugin_id"},
	)

	scanRunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "zap_operator_scan_runs_total",
			Help: "Total number of ZAP scan runs completed.",
		},
		[]string{"scan_target", "scan_namespace", "status"},
	)

	scanDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "zap_operator_scan_duration_seconds",
			Help:    "Duration of ZAP scans in seconds.",
			Buckets: []float64{60, 120, 300, 600, 900, 1200, 1800, 3600, 7200},
		},
		[]string{"scan_target", "scan_namespace"},
	)

	scansInProgress = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zap_operator_scans_in_progress",
			Help: "Number of ZAP scans currently in progress.",
		},
		[]string{"scan_namespace"},
	)

	lastScanTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zap_operator_last_scan_timestamp_seconds",
			Help: "Unix timestamp of the last completed scan.",
		},
		[]string{"scan_target", "scan_namespace", "status"},
	)

	lastScanDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "zap_operator_last_scan_duration_seconds",
			Help: "Duration of the last completed scan in seconds.",
		},
		[]string{"scan_target", "scan_namespace"},
	)

	registerOnce sync.Once
)

func Register(registry prometheus.Registerer) {
	registerOnce.Do(func() {
		registry.MustRegister(
			alertsFound,
			scanRunsTotal,
			scanDurationSeconds,
			scansInProgress,
			lastScanTimestamp,
			lastScanDuration,
		)
	})
}

func IncAlert(scanNamespace, scanTarget, risk, pluginID string, count int) {
	if count <= 0 {
		return
	}
	alertsFound.WithLabelValues(scanTarget, scanNamespace, risk, pluginID).Add(float64(count))
}

// IncScanRun increments the scan runs counter.
// status should be "succeeded" or "failed".
func IncScanRun(scanNamespace, scanTarget, status string) {
	scanRunsTotal.WithLabelValues(scanTarget, scanNamespace, status).Inc()
}

// ObserveScanDuration records the duration of a completed scan.
func ObserveScanDuration(scanNamespace, scanTarget string, durationSeconds float64) {
	if durationSeconds > 0 {
		scanDurationSeconds.WithLabelValues(scanTarget, scanNamespace).Observe(durationSeconds)
		lastScanDuration.WithLabelValues(scanTarget, scanNamespace).Set(durationSeconds)
	}
}

// SetScansInProgress sets the number of scans in progress for a namespace.
func SetScansInProgress(scanNamespace string, count float64) {
	scansInProgress.WithLabelValues(scanNamespace).Set(count)
}

// IncScansInProgress increments the in-progress scan count for a namespace.
func IncScansInProgress(scanNamespace string) {
	scansInProgress.WithLabelValues(scanNamespace).Inc()
}

// DecScansInProgress decrements the in-progress scan count for a namespace.
func DecScansInProgress(scanNamespace string) {
	scansInProgress.WithLabelValues(scanNamespace).Dec()
}

// SetLastScanTimestamp records the timestamp of the last completed scan.
func SetLastScanTimestamp(scanNamespace, scanTarget, status string, timestamp float64) {
	lastScanTimestamp.WithLabelValues(scanTarget, scanNamespace, status).Set(timestamp)
}
