package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

func createTestCapture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	meta := &recv.Metadata{
		Version:    1,
		Format:     "jsonl",
		Started:    time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		Stopped:    time.Date(2026, 2, 20, 10, 30, 0, 0, time.UTC),
		TotalLines: 100,
		TotalBytes: 5000,
		LabelsSeen: []string{"app", "namespace"},
	}
	if err := recv.WriteMetadata(dir, meta); err != nil {
		t.Fatal(err)
	}

	// Write some JSONL lines for triage to scan
	f, err := os.Create(filepath.Join(dir, "test.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 100; i++ {
		line := `{"ts":"` + ts.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano) + `","labels":{"app":"test"},"msg":"line ` + string(rune('0'+i%10)) + `"}`
		_, _ = f.WriteString(line + "\n")
	}
	_ = f.Close()

	return dir
}

func TestReport_JSON(t *testing.T) {
	dir := createTestCapture(t)

	result, err := Report(dir, ReportConfig{Jobs: 1, Top: 5}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := result.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}

	var parsed ReportResult
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed.Capture.Dir != dir {
		t.Errorf("dir = %q, want %q", parsed.Capture.Dir, dir)
	}
	if parsed.Severity == "" {
		t.Error("expected non-empty severity")
	}
	if len(parsed.Suggested) == 0 {
		t.Error("expected at least one suggested command")
	}
}

func TestReport_Severity(t *testing.T) {
	tests := []struct {
		name   string
		rate   float64
		errors []ErrorSignature
		want   string
	}{
		{"low_rate", 0.5, nil, "low"},
		{"medium_rate", 2.5, nil, "medium"},
		{"high_rate", 10.0, nil, "high"},
		{"oom_pattern", 0.1, []ErrorSignature{{Signature: "OOMKilled"}}, "high"},
		{"panic_pattern", 0.1, []ErrorSignature{{Signature: "goroutine panic"}}, "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySeverity(tt.rate, tt.errors)
			if got != tt.want {
				t.Errorf("classifySeverity(%.1f) = %q, want %q", tt.rate, got, tt.want)
			}
		})
	}
}

func TestReport_ContainsAny(t *testing.T) {
	if !containsAny("OOMKilled in pod-abc", "OOMKilled") {
		t.Error("expected match for OOMKilled")
	}
	if containsAny("normal log line", "OOMKilled", "panic") {
		t.Error("expected no match")
	}
	if !containsAny("goroutine 1 [running]: panic", "panic") {
		t.Error("expected match for panic")
	}
}

func TestReport_HTML(t *testing.T) {
	dir := createTestCapture(t)

	result, err := Report(dir, ReportConfig{Jobs: 1, Top: 5}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := result.WriteHTML(&buf, nil, nil); err != nil {
		t.Fatal(err)
	}

	html := buf.String()
	if len(html) == 0 {
		t.Error("expected non-empty HTML")
	}
	if !bytes.Contains(buf.Bytes(), []byte("<!DOCTYPE html>")) {
		t.Error("expected HTML doctype")
	}
	if !bytes.Contains(buf.Bytes(), []byte("incident report")) {
		t.Error("expected 'incident report' in HTML")
	}
}
