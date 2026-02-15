package recv

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestPatternNames(t *testing.T) {
	r, err := NewRedactor([]string{"email", "credit_card"})
	if err != nil {
		t.Fatal(err)
	}

	names := r.PatternNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "email" && names[1] != "email" {
		t.Errorf("expected email in names: %v", names)
	}
}

func TestPatternNamesAll(t *testing.T) {
	r, err := NewRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}
	names := r.PatternNames()
	if len(names) != len(builtinPatterns) {
		t.Errorf("expected %d names, got %d", len(builtinPatterns), len(names))
	}
}

func TestLuhnNonDigitChar(t *testing.T) {
	// luhnValid should return false for strings with non-digit, non-space, non-dash chars
	if luhnValid("4111x111x111x1111") {
		t.Error("expected false for string with letters")
	}
	if luhnValid("4111.1111.1111.1111") {
		t.Error("expected false for string with dots")
	}
}

func TestLuhnTooShort(t *testing.T) {
	if luhnValid("123456") {
		t.Error("expected false for short string")
	}
}

func TestLuhnTooLong(t *testing.T) {
	if luhnValid("12345678901234567890") {
		t.Error("expected false for 20-digit string")
	}
}

func TestLoadCustomPatternsFileNotFound(t *testing.T) {
	r, err := NewRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}
	err = r.LoadCustomPatterns("/nonexistent/patterns.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadCustomPatternsInvalidYAML(t *testing.T) {
	r, err := NewRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("{{{{invalid yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = r.LoadCustomPatterns(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadCustomPatternsInvalidRegex(t *testing.T) {
	r, err := NewRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_regex.yaml")
	yaml := `- name: broken
  pattern: "[invalid(regex"
  replacement: "[REDACTED]"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	err = r.LoadCustomPatterns(path)
	if err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

func TestStripPort(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"192.168.1.1:8080", "192.168.1.1"},
		{"localhost:3000", "localhost"},
		{"no-port-here", "no-port-here"},
		{":8080", ""},
	}
	for _, tt := range tests {
		got := stripPort(tt.input)
		if got != tt.want {
			t.Errorf("stripPort(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseNanoTimestampValid(t *testing.T) {
	ts := parseNanoTimestamp("1704067200000000000")
	if ts.Year() != 2024 {
		t.Errorf("expected 2024, got %d", ts.Year())
	}
}

func TestParseNanoTimestampInvalid(t *testing.T) {
	before := time.Now()
	ts := parseNanoTimestamp("not-a-number")
	after := time.Now()
	if ts.Before(before) || ts.After(after) {
		t.Error("invalid timestamp should return time.Now()")
	}
}

func TestParseNanoTimestampWhitespace(t *testing.T) {
	ts := parseNanoTimestamp("  1704067200000000000  ")
	if ts.Year() != 2024 {
		t.Errorf("expected 2024, got %d", ts.Year())
	}
}

func TestRawPushWithRedaction(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	redactor, err := NewRedactor([]string{"email"})
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer(":0", w, redactor, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	payload := `{"ts":"2024-01-01T00:00:00Z","msg":"user test@example.com logged in"}`
	resp, err := http.Post(ts.URL+"/logtap/raw", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	time.Sleep(50 * time.Millisecond)
	w.Close()

	output := buf.String()
	if strings.Contains(output, "test@example.com") {
		t.Error("email not redacted in raw push")
	}
	if !strings.Contains(output, "[REDACTED:email]") {
		t.Error("expected redaction marker in raw push")
	}
}

func TestRawPushZeroTimestamp(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	// send entry without timestamp
	payload := `{"msg":"no timestamp"}`
	resp, err := http.Post(ts.URL+"/logtap/raw", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	time.Sleep(50 * time.Millisecond)
	w.Close()

	if !strings.Contains(buf.String(), "no timestamp") {
		t.Error("message not written")
	}
}

func TestRawPushInvalidJSON(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/logtap/raw", "application/json", strings.NewReader("{bad json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRawPushWithMetricsAndStats(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	stats := NewStats()
	ring := NewLogRing(100)

	srv := NewServer(":0", w, nil, m, stats, ring)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	payload := `{"ts":"2024-01-01T00:00:00Z","labels":{"app":"test"},"msg":"tracked"}`
	resp, err := http.Post(ts.URL+"/logtap/raw", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	time.Sleep(50 * time.Millisecond)
	w.Close()

	if buf.Len() == 0 {
		t.Error("no output written")
	}
}

func TestRawPushBackpressure(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1, &buf, nil) // tiny buffer
	defer w.Close()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	stats := NewStats()

	srv := NewServer(":0", w, nil, m, stats, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	// send many entries in a single raw push
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, `{"ts":"2024-01-01T00:00:00Z","msg":"flood"}`)
	}
	payload := strings.Join(lines, "\n")

	resp, err := http.Post(ts.URL+"/logtap/raw", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	// should still return 204
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestSetAuditLogger(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, nil, nil)

	dir := t.TempDir()
	al, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	srv.SetAuditLogger(al)
	if srv.audit == nil {
		t.Error("audit logger should be set")
	}
}
