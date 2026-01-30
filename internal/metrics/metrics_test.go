package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRegister(t *testing.T) {
	// Register is idempotent due to sync.Once, so this just verifies it doesn't panic
	reg := prometheus.NewRegistry()
	Register(reg)
	// Call again to ensure idempotency
	Register(reg)
}

func TestIncAlert(t *testing.T) {
	// Reset the counter for testing
	alertsFound.Reset()

	// Test with valid count
	IncAlert("ns1", "https://example.com", "high", "10038", 5)

	expected := `
		# HELP zap_operator_alerts_found_total Total number of ZAP alerts found by zap-full-scan jobs.
		# TYPE zap_operator_alerts_found_total counter
		zap_operator_alerts_found_total{plugin_id="10038",risk="high",scan_namespace="ns1",scan_target="https://example.com"} 5
	`
	if err := testutil.CollectAndCompare(alertsFound, strings.NewReader(expected)); err != nil {
		t.Errorf("unexpected metric result: %v", err)
	}

	// Test with zero count (should not increment)
	IncAlert("ns1", "https://example.com", "high", "10038", 0)

	// Should still be 5
	if err := testutil.CollectAndCompare(alertsFound, strings.NewReader(expected)); err != nil {
		t.Errorf("unexpected metric result after zero count: %v", err)
	}

	// Test with negative count (should not increment)
	IncAlert("ns1", "https://example.com", "high", "10038", -1)

	// Should still be 5
	if err := testutil.CollectAndCompare(alertsFound, strings.NewReader(expected)); err != nil {
		t.Errorf("unexpected metric result after negative count: %v", err)
	}
}

func TestIncScanRun(t *testing.T) {
	scanRunsTotal.Reset()

	IncScanRun("ns1", "https://example.com", "succeeded")
	IncScanRun("ns1", "https://example.com", "succeeded")
	IncScanRun("ns1", "https://example.com", "failed")

	val := testutil.ToFloat64(scanRunsTotal.WithLabelValues("https://example.com", "ns1", "succeeded"))
	if val != 2 {
		t.Errorf("expected succeeded count 2, got %v", val)
	}

	val = testutil.ToFloat64(scanRunsTotal.WithLabelValues("https://example.com", "ns1", "failed"))
	if val != 1 {
		t.Errorf("expected failed count 1, got %v", val)
	}
}

func TestObserveScanDuration(t *testing.T) {
	scanDurationSeconds.Reset()
	lastScanDuration.Reset()

	// Test with valid duration
	ObserveScanDuration("ns1", "https://example.com", 120.5)

	// Check last scan duration gauge
	val := testutil.ToFloat64(lastScanDuration.WithLabelValues("https://example.com", "ns1"))
	if val != 120.5 {
		t.Errorf("expected last scan duration 120.5, got %v", val)
	}

	// Test with zero duration (should not record)
	lastScanDuration.Reset()
	ObserveScanDuration("ns2", "https://test.com", 0)

	val = testutil.ToFloat64(lastScanDuration.WithLabelValues("https://test.com", "ns2"))
	if val != 0 {
		t.Errorf("expected 0 for zero duration, got %v", val)
	}

	// Test with negative duration (should not record)
	ObserveScanDuration("ns2", "https://test.com", -10)

	val = testutil.ToFloat64(lastScanDuration.WithLabelValues("https://test.com", "ns2"))
	if val != 0 {
		t.Errorf("expected 0 for negative duration, got %v", val)
	}
}

func TestScansInProgress(t *testing.T) {
	scansInProgress.Reset()

	// Test SetScansInProgress
	SetScansInProgress("ns1", 5)
	val := testutil.ToFloat64(scansInProgress.WithLabelValues("ns1"))
	if val != 5 {
		t.Errorf("expected 5, got %v", val)
	}

	// Test IncScansInProgress
	IncScansInProgress("ns1")
	val = testutil.ToFloat64(scansInProgress.WithLabelValues("ns1"))
	if val != 6 {
		t.Errorf("expected 6, got %v", val)
	}

	// Test DecScansInProgress
	DecScansInProgress("ns1")
	DecScansInProgress("ns1")
	val = testutil.ToFloat64(scansInProgress.WithLabelValues("ns1"))
	if val != 4 {
		t.Errorf("expected 4, got %v", val)
	}
}

func TestSetLastScanTimestamp(t *testing.T) {
	lastScanTimestamp.Reset()

	ts := float64(1700000000)
	SetLastScanTimestamp("ns1", "https://example.com", "succeeded", ts)

	val := testutil.ToFloat64(lastScanTimestamp.WithLabelValues("https://example.com", "ns1", "succeeded"))
	if val != ts {
		t.Errorf("expected %v, got %v", ts, val)
	}

	// Update with new timestamp
	ts2 := float64(1700001000)
	SetLastScanTimestamp("ns1", "https://example.com", "succeeded", ts2)

	val = testutil.ToFloat64(lastScanTimestamp.WithLabelValues("https://example.com", "ns1", "succeeded"))
	if val != ts2 {
		t.Errorf("expected %v, got %v", ts2, val)
	}
}
