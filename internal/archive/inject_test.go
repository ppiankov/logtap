package archive

import (
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

func TestParseFault_ErrorSpike(t *testing.T) {
	fc, err := ParseFault("error-spike")
	if err != nil {
		t.Fatal(err)
	}
	if fc.Type != FaultErrorSpike {
		t.Errorf("type = %q, want %q", fc.Type, FaultErrorSpike)
	}
	if fc.Service != "" {
		t.Errorf("service = %q, want empty", fc.Service)
	}
}

func TestParseFault_ServiceDown(t *testing.T) {
	fc, err := ParseFault("service-down=payment")
	if err != nil {
		t.Fatal(err)
	}
	if fc.Type != FaultServiceDown {
		t.Errorf("type = %q, want %q", fc.Type, FaultServiceDown)
	}
	if fc.Service != "payment" {
		t.Errorf("service = %q, want %q", fc.Service, "payment")
	}
}

func TestParseFault_LatencySpike(t *testing.T) {
	fc, err := ParseFault("latency-spike=api")
	if err != nil {
		t.Fatal(err)
	}
	if fc.Type != FaultLatencySpike {
		t.Errorf("type = %q, want %q", fc.Type, FaultLatencySpike)
	}
	if fc.Service != "api" {
		t.Errorf("service = %q, want %q", fc.Service, "api")
	}
}

func TestParseFault_Invalid(t *testing.T) {
	cases := []string{
		"unknown-fault",
		"service-down",       // missing service
		"latency-spike",      // missing service
		"error-spike=server", // unexpected param
	}
	for _, spec := range cases {
		if _, err := ParseFault(spec); err == nil {
			t.Errorf("ParseFault(%q) should have returned error", spec)
		}
	}
}

func TestInjector_ErrorSpike(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	faults := []FaultConfig{
		{Type: FaultErrorSpike, At: base, Duration: 10 * time.Second},
	}
	inject := NewInjector(faults)

	entry := recv.LogEntry{
		Timestamp: base.Add(5 * time.Second),
		Labels:    map[string]string{"app": "web"},
		Message:   "normal request",
	}

	result := inject(entry)
	// original + 3 synthetic errors
	if len(result) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(result))
	}
	if result[0].Message != "normal request" {
		t.Errorf("first entry should be original, got %q", result[0].Message)
	}
}

func TestInjector_ServiceDown(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	faults := []FaultConfig{
		{Type: FaultServiceDown, Service: "payment", At: base, Duration: 10 * time.Second},
	}
	inject := NewInjector(faults)

	// entry from the downed service — should be dropped
	entry := recv.LogEntry{
		Timestamp: base.Add(5 * time.Second),
		Labels:    map[string]string{"app": "payment"},
		Message:   "processing order",
	}
	result := inject(entry)
	if len(result) != 0 {
		t.Errorf("expected 0 entries for downed service, got %d", len(result))
	}

	// entry from another service — should pass through
	other := recv.LogEntry{
		Timestamp: base.Add(5 * time.Second),
		Labels:    map[string]string{"app": "web"},
		Message:   "handling request",
	}
	result = inject(other)
	if len(result) != 1 {
		t.Errorf("expected 1 entry for other service, got %d", len(result))
	}
}

func TestInjector_LatencySpike(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	faults := []FaultConfig{
		{Type: FaultLatencySpike, Service: "api", At: base, Duration: 10 * time.Second},
	}
	inject := NewInjector(faults)

	entry := recv.LogEntry{
		Timestamp: base.Add(5 * time.Second),
		Labels:    map[string]string{"app": "api"},
		Message:   "GET /users",
	}
	result := inject(entry)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Message != "GET /users [SLOW: 5.2s]" {
		t.Errorf("message = %q, want %q", result[0].Message, "GET /users [SLOW: 5.2s]")
	}
}

func TestInjector_OutsideWindow(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	faults := []FaultConfig{
		{Type: FaultErrorSpike, At: base, Duration: 10 * time.Second},
	}
	inject := NewInjector(faults)

	// before window
	before := recv.LogEntry{
		Timestamp: base.Add(-1 * time.Second),
		Labels:    map[string]string{"app": "web"},
		Message:   "before",
	}
	if result := inject(before); len(result) != 1 {
		t.Errorf("before window: expected 1 entry, got %d", len(result))
	}

	// after window
	after := recv.LogEntry{
		Timestamp: base.Add(11 * time.Second),
		Labels:    map[string]string{"app": "web"},
		Message:   "after",
	}
	if result := inject(after); len(result) != 1 {
		t.Errorf("after window: expected 1 entry, got %d", len(result))
	}
}

func TestInjector_InjectedLabel(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		faults []FaultConfig
		entry  recv.LogEntry
	}{
		{
			name:   "error-spike",
			faults: []FaultConfig{{Type: FaultErrorSpike, At: base, Duration: 10 * time.Second}},
			entry: recv.LogEntry{
				Timestamp: base.Add(5 * time.Second),
				Labels:    map[string]string{"app": "web"},
				Message:   "request",
			},
		},
		{
			name:   "latency-spike",
			faults: []FaultConfig{{Type: FaultLatencySpike, Service: "api", At: base, Duration: 10 * time.Second}},
			entry: recv.LogEntry{
				Timestamp: base.Add(5 * time.Second),
				Labels:    map[string]string{"app": "api"},
				Message:   "GET /data",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inject := NewInjector(tt.faults)
			result := inject(tt.entry)

			for i, e := range result {
				if i == 0 && tt.name == "error-spike" {
					// original entry should NOT have _injected
					if e.Labels["_injected"] == "true" {
						t.Errorf("original entry should not have _injected label")
					}
					continue
				}
				if e.Labels["_injected"] != "true" {
					t.Errorf("entry %d missing _injected label: %v", i, e.Labels)
				}
			}
		})
	}
}
