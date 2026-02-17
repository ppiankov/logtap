package recv

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func gatherMetric(t *testing.T, reg *prometheus.Registry, name string) *dto.MetricFamily {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() == name {
			return f
		}
	}
	return nil
}

func TestNewMetrics_Registration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Initialize labeled metrics so they appear in gather
	m.RedactionsTotal.WithLabelValues("test")
	m.RotationTotal.WithLabelValues("test")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	expected := map[string]bool{
		"logtap_logs_received_total":       false,
		"logtap_logs_dropped_total":        false,
		"logtap_bytes_written_total":       false,
		"logtap_disk_usage_bytes":          false,
		"logtap_active_connections":        false,
		"logtap_backpressure_events_total": false,
		"logtap_redactions_total":          false,
		"logtap_push_duration_seconds":     false,
		"logtap_writer_queue_length":       false,
		"logtap_rotation_total":            false,
		"logtap_rotation_errors_total":     false,
	}

	for _, f := range families {
		if _, ok := expected[f.GetName()]; ok {
			expected[f.GetName()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("metric %q not registered", name)
		}
	}
}

func TestMetrics_Counters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.LogsReceived.Add(10)
	m.LogsDropped.Add(3)
	m.BytesWritten.Add(1024)
	m.BackpressureEvents.Add(2)
	m.RotationErrors.Add(1)

	tests := []struct {
		name  string
		value float64
	}{
		{"logtap_logs_received_total", 10},
		{"logtap_logs_dropped_total", 3},
		{"logtap_bytes_written_total", 1024},
		{"logtap_backpressure_events_total", 2},
		{"logtap_rotation_errors_total", 1},
	}

	for _, tt := range tests {
		f := gatherMetric(t, reg, tt.name)
		if f == nil {
			t.Errorf("metric %q not found", tt.name)
			continue
		}
		got := f.GetMetric()[0].GetCounter().GetValue()
		if got != tt.value {
			t.Errorf("%s = %v, want %v", tt.name, got, tt.value)
		}
	}
}

func TestMetrics_Gauges(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.DiskUsage.Set(5000)
	m.ActiveConnections.Set(3)
	m.WriterQueueLength.Set(42)

	tests := []struct {
		name  string
		value float64
	}{
		{"logtap_disk_usage_bytes", 5000},
		{"logtap_active_connections", 3},
		{"logtap_writer_queue_length", 42},
	}

	for _, tt := range tests {
		f := gatherMetric(t, reg, tt.name)
		if f == nil {
			t.Errorf("metric %q not found", tt.name)
			continue
		}
		got := f.GetMetric()[0].GetGauge().GetValue()
		if got != tt.value {
			t.Errorf("%s = %v, want %v", tt.name, got, tt.value)
		}
	}
}

func TestMetrics_RedactionsTotal(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RedactionsTotal.WithLabelValues("email").Add(5)
	m.RedactionsTotal.WithLabelValues("jwt").Add(2)

	f := gatherMetric(t, reg, "logtap_redactions_total")
	if f == nil {
		t.Fatal("logtap_redactions_total not found")
	}

	byLabel := make(map[string]float64)
	for _, metric := range f.GetMetric() {
		for _, lp := range metric.GetLabel() {
			if lp.GetName() == "pattern" {
				byLabel[lp.GetValue()] = metric.GetCounter().GetValue()
			}
		}
	}

	if byLabel["email"] != 5 {
		t.Errorf("email = %v, want 5", byLabel["email"])
	}
	if byLabel["jwt"] != 2 {
		t.Errorf("jwt = %v, want 2", byLabel["jwt"])
	}
}

func TestMetrics_RotationTotal(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RotationTotal.WithLabelValues("size").Add(3)
	m.RotationTotal.WithLabelValues("time").Add(1)

	f := gatherMetric(t, reg, "logtap_rotation_total")
	if f == nil {
		t.Fatal("logtap_rotation_total not found")
	}

	byLabel := make(map[string]float64)
	for _, metric := range f.GetMetric() {
		for _, lp := range metric.GetLabel() {
			if lp.GetName() == "reason" {
				byLabel[lp.GetValue()] = metric.GetCounter().GetValue()
			}
		}
	}

	if byLabel["size"] != 3 {
		t.Errorf("size = %v, want 3", byLabel["size"])
	}
	if byLabel["time"] != 1 {
		t.Errorf("time = %v, want 1", byLabel["time"])
	}
}

func TestMetrics_Histogram(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.PushDuration.Observe(0.05)
	m.PushDuration.Observe(0.1)
	m.PushDuration.Observe(0.5)

	f := gatherMetric(t, reg, "logtap_push_duration_seconds")
	if f == nil {
		t.Fatal("logtap_push_duration_seconds not found")
	}

	h := f.GetMetric()[0].GetHistogram()
	if h.GetSampleCount() != 3 {
		t.Errorf("sample count = %d, want 3", h.GetSampleCount())
	}
	if h.GetSampleSum() < 0.64 || h.GetSampleSum() > 0.66 {
		t.Errorf("sample sum = %v, want ~0.65", h.GetSampleSum())
	}
}
