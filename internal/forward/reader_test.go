package forward

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		input   string
		wantMsg string
		wantTS  bool // whether timestamp should parse successfully
	}{
		{
			input:   "2024-01-15T10:30:00.123456789Z hello world",
			wantMsg: "hello world",
			wantTS:  true,
		},
		{
			input:   "2024-01-15T10:30:00Z single word",
			wantMsg: "single word",
			wantTS:  true,
		},
		{
			input:   "no-timestamp-here",
			wantMsg: "no-timestamp-here",
			wantTS:  false,
		},
		{
			input:   "bad-ts actual message",
			wantMsg: "bad-ts actual message",
			wantTS:  false,
		},
	}

	for _, tt := range tests {
		ts, msg := ParseLogLine(tt.input)
		if msg != tt.wantMsg {
			t.Errorf("ParseLogLine(%q) msg = %q, want %q", tt.input, msg, tt.wantMsg)
		}
		if tt.wantTS {
			if ts.Year() != 2024 {
				t.Errorf("ParseLogLine(%q) year = %d, want 2024", tt.input, ts.Year())
			}
		} else {
			// should fall back to time.Now()
			if time.Since(ts) > time.Second {
				t.Errorf("ParseLogLine(%q) fallback timestamp too old: %v", tt.input, ts)
			}
		}
	}
}

func TestFilterContainers(t *testing.T) {
	containers := []corev1.Container{
		{Name: "app"},
		{Name: "logtap-forwarder-lt-a3f9"},
		{Name: "sidecar-proxy"},
		{Name: "logtap-forwarder-lt-b2c1"},
	}

	got := FilterContainers(containers)
	if len(got) != 2 {
		t.Fatalf("FilterContainers = %v, want 2 containers", got)
	}
	if got[0] != "app" {
		t.Errorf("got[0] = %q, want %q", got[0], "app")
	}
	if got[1] != "sidecar-proxy" {
		t.Errorf("got[1] = %q, want %q", got[1], "sidecar-proxy")
	}
}

func TestFilterContainers_AllForwarders(t *testing.T) {
	containers := []corev1.Container{
		{Name: "logtap-forwarder-lt-a3f9"},
	}

	got := FilterContainers(containers)
	if len(got) != 0 {
		t.Errorf("FilterContainers = %v, want empty", got)
	}
}
