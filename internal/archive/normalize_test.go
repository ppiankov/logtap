package archive

import "testing"

func TestNormalizeUUID(t *testing.T) {
	input := "request id=a1b2c3d4-e5f6-7890-abcd-ef1234567890 failed"
	got := NormalizeMessage(input)
	want := "request id=<UUID> failed"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeIP(t *testing.T) {
	input := "connection refused to 10.0.1.42:8080"
	got := NormalizeMessage(input)
	want := "connection refused to <IP>:<N>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeTimestamp(t *testing.T) {
	input := "event at 2024-01-15T10:32:01Z processed"
	got := NormalizeMessage(input)
	want := "event at <TS> processed"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeTimestampFractional(t *testing.T) {
	input := "logged at 2024-01-15T10:32:01.123456Z"
	got := NormalizeMessage(input)
	want := "logged at <TS>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeHex(t *testing.T) {
	input := "memory at 0x7ffeefbff4a0 corrupted"
	got := NormalizeMessage(input)
	want := "memory at <HEX> corrupted"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeDuration(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"took 230ms", "took <DUR>"},
		{"duration=1.5s", "duration=<DUR>"},
		{"timeout after 30s", "timeout after <DUR>"},
	}
	for _, tt := range tests {
		got := NormalizeMessage(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeMessage(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeNumbers(t *testing.T) {
	// 4+ digit numbers become <N>, shorter ones preserved
	tests := []struct {
		input string
		want  string
	}{
		{"port 8080", "port <N>"},
		{"status 200", "status 200"},
		{"status 500", "status 500"},
		{"processed 14832901 lines", "processed <N> lines"},
	}
	for _, tt := range tests {
		got := NormalizeMessage(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeMessage(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeCombined(t *testing.T) {
	input := "2024-01-15T10:32:01Z [api] request a1b2c3d4-e5f6-7890-abcd-ef1234567890 to 10.0.1.42:8080 failed after 230ms"
	got := NormalizeMessage(input)
	want := "<TS> [api] request <UUID> to <IP>:<N> failed after <DUR>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeIdempotent(t *testing.T) {
	input := "connection to 10.0.1.42 refused after 230ms for request a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	first := NormalizeMessage(input)
	second := NormalizeMessage(first)
	if first != second {
		t.Errorf("not idempotent:\nfirst:  %q\nsecond: %q", first, second)
	}
}

func TestIsError(t *testing.T) {
	positive := []string{
		"connection error to database",
		"PANIC: nil pointer dereference",
		"Fatal: cannot bind port",
		"java.lang.NullPointerException",
		"request failed with status 503",
		"connection refused to payments-service:8080",
		"context deadline exceeded",
		"OOMKilled container api",
		"CrashLoopBackOff for pod worker-abc",
		"segfault at 0x0",
		"x509: certificate has expired",
		"upstream timeout after 30s",
	}
	for _, msg := range positive {
		if !IsError(msg) {
			t.Errorf("IsError(%q) = false, want true", msg)
		}
	}

	negative := []string{
		"request started",
		"job completed successfully",
		"health check ok",
		"GET /api/v1/users 200 12ms",
		"processing order",
	}
	for _, msg := range negative {
		if IsError(msg) {
			t.Errorf("IsError(%q) = true, want false", msg)
		}
	}
}

func TestNormalizePreservesShortTokens(t *testing.T) {
	// HTTP status codes and short numbers should be preserved
	input := "POST /api/v1/users 201 12ms"
	got := NormalizeMessage(input)
	want := "POST /api/v1/users 201 <DUR>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
