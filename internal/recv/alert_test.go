package recv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type alertCapture struct {
	mu     sync.Mutex
	events []WebhookEvent
}

func (c *alertCapture) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var evt WebhookEvent
		if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
			w.WriteHeader(400)
			return
		}
		c.mu.Lock()
		c.events = append(c.events, evt)
		c.mu.Unlock()
		w.WriteHeader(200)
	})
}

func (c *alertCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func (c *alertCapture) lastDetail() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		return ""
	}
	return c.events[len(c.events)-1].Detail
}

func newAlertTestSetup(t *testing.T, rules []AlertRule) (*AlertEngine, *alertCapture, func()) {
	t.Helper()
	capture := &alertCapture{}
	srv := httptest.NewServer(capture.handler())
	disp := NewWebhookDispatcher([]string{srv.URL}, nil)
	engine := NewAlertEngine(rules, disp)
	return engine, capture, srv.Close
}

func TestAlertEngine_DropRate(t *testing.T) {
	rules := []AlertRule{{
		Name:      "high_drops",
		Metric:    "drop_rate",
		Op:        "gt",
		Threshold: 10,
		Detail:    "drops exceeded 10/s",
	}}
	engine, capture, cleanup := newAlertTestSetup(t, rules)
	defer cleanup()

	// First eval establishes baseline
	engine.Evaluate(Snapshot{LogsDropped: 0})

	// Second eval: 20 drops in 1 tick = 20/s rate
	engine.Evaluate(Snapshot{LogsDropped: 20})

	// Wait for async webhook
	waitForAlerts(t, capture, 1)
	if capture.lastDetail() != "drops exceeded 10/s" {
		t.Errorf("detail = %q", capture.lastDetail())
	}
}

func TestAlertEngine_DiskPct(t *testing.T) {
	rules := []AlertRule{{
		Name:      "disk_full",
		Metric:    "disk_pct",
		Op:        "gt",
		Threshold: 90,
		Detail:    "disk above 90%",
	}}
	engine, capture, cleanup := newAlertTestSetup(t, rules)
	defer cleanup()

	// 91% full
	engine.Evaluate(Snapshot{DiskUsage: 91, DiskCap: 100})

	waitForAlerts(t, capture, 1)
	if capture.lastDetail() != "disk above 90%" {
		t.Errorf("detail = %q", capture.lastDetail())
	}
}

func TestAlertEngine_Hysteresis(t *testing.T) {
	rules := []AlertRule{{
		Name:      "high_drops",
		Metric:    "logs_dropped",
		Op:        "gt",
		Threshold: 100,
		Detail:    "too many drops",
	}}
	engine, capture, cleanup := newAlertTestSetup(t, rules)
	defer cleanup()

	// Trigger
	engine.Evaluate(Snapshot{LogsDropped: 200})
	waitForAlerts(t, capture, 1)

	// Still above threshold — should NOT re-fire
	engine.Evaluate(Snapshot{LogsDropped: 300})
	engine.Evaluate(Snapshot{LogsDropped: 400})

	// Count should still be 1
	if capture.count() != 1 {
		t.Errorf("fired %d times, want 1 (hysteresis)", capture.count())
	}
}

func TestAlertEngine_Resolve(t *testing.T) {
	rules := []AlertRule{{
		Name:      "high_drops",
		Metric:    "logs_dropped",
		Op:        "gt",
		Threshold: 100,
		Detail:    "too many drops",
	}}
	engine, capture, cleanup := newAlertTestSetup(t, rules)
	defer cleanup()

	// Trigger
	engine.Evaluate(Snapshot{LogsDropped: 200})
	waitForAlerts(t, capture, 1)

	// Resolve (below threshold)
	engine.Evaluate(Snapshot{LogsDropped: 50})
	if len(engine.Fired()) != 0 {
		t.Error("expected fired to be empty after resolve")
	}

	// Re-trigger
	engine.Evaluate(Snapshot{LogsDropped: 200})
	waitForAlerts(t, capture, 2)
}

func TestAlertRules_Parse(t *testing.T) {
	dir := t.TempDir()
	content := `
rules:
  - name: high_drops
    metric: drop_rate
    op: gt
    threshold: 100
    detail: "Drop rate exceeded"
  - name: disk_full
    metric: disk_pct
    op: gt
    threshold: 90
    detail: "Disk full"
`
	path := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	rules, err := LoadAlertRules(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("len = %d, want 2", len(rules))
	}
	if rules[0].Name != "high_drops" {
		t.Errorf("rules[0].Name = %q", rules[0].Name)
	}
	if rules[1].Threshold != 90 {
		t.Errorf("rules[1].Threshold = %v", rules[1].Threshold)
	}
}

func TestAlertRules_InvalidMetric(t *testing.T) {
	dir := t.TempDir()
	content := `
rules:
  - name: bad
    metric: unknown_metric
    op: gt
    threshold: 10
`
	path := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadAlertRules(path)
	if err == nil {
		t.Error("expected error for unknown metric")
	}
}

func TestAlertRules_InvalidOp(t *testing.T) {
	dir := t.TempDir()
	content := `
rules:
  - name: bad
    metric: disk_pct
    op: eq
    threshold: 10
`
	path := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadAlertRules(path)
	if err == nil {
		t.Error("expected error for unknown operator")
	}
}

func TestAlertRules_MissingName(t *testing.T) {
	dir := t.TempDir()
	content := `
rules:
  - metric: disk_pct
    op: gt
    threshold: 10
`
	path := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadAlertRules(path)
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestAlertEngine_NilDispatcher(t *testing.T) {
	engine := NewAlertEngine([]AlertRule{{
		Name:      "test",
		Metric:    "disk_pct",
		Op:        "gt",
		Threshold: 90,
	}}, nil)
	// Should not panic
	engine.Evaluate(Snapshot{DiskUsage: 95, DiskCap: 100})
}

func TestCompare(t *testing.T) {
	tests := []struct {
		val       float64
		op        string
		threshold float64
		want      bool
	}{
		{10, "gt", 5, true},
		{5, "gt", 10, false},
		{10, "lt", 15, true},
		{15, "lt", 10, false},
		{10, "gte", 10, true},
		{10, "lte", 10, true},
		{9, "gte", 10, false},
		{11, "lte", 10, false},
	}
	for _, tt := range tests {
		got := compare(tt.val, tt.op, tt.threshold)
		if got != tt.want {
			t.Errorf("compare(%v, %q, %v) = %v, want %v", tt.val, tt.op, tt.threshold, got, tt.want)
		}
	}
}

// waitForAlerts polls the capture until the expected count is reached.
func waitForAlerts(t *testing.T, c *alertCapture, want int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if c.count() >= want {
			return
		}
		// busy wait — webhook is async
		for j := 0; j < 1000000; j++ {
		}
	}
	t.Fatalf("timed out waiting for %d alerts, got %d", want, c.count())
}
