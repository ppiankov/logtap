package main

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/cli"
	"github.com/ppiankov/logtap/internal/recv"
)

// makeRegressionCaptures creates a baseline (low errors) and current (high errors + new patterns).
func makeRegressionCaptures(t *testing.T) (baselineDir, currentDir string) {
	t.Helper()
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	// Baseline: 20 lines, 1 error (5% error rate)
	baselineEntries := make([]recv.LogEntry, 20)
	for i := range baselineEntries {
		msg := fmt.Sprintf("normal line %d", i)
		if i == 0 {
			msg = "ERROR: connection refused"
		}
		baselineEntries[i] = recv.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Labels:    map[string]string{"app": "web"},
			Message:   msg,
		}
	}

	// Current: 20 lines, 10 errors (50% error rate) with new patterns
	currentEntries := make([]recv.LogEntry, 20)
	for i := range currentEntries {
		msg := fmt.Sprintf("normal line %d", i)
		if i < 5 {
			msg = "ERROR: connection refused"
		}
		if i >= 5 && i < 10 {
			msg = "FATAL: out of memory"
		}
		currentEntries[i] = recv.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Labels:    map[string]string{"app": "web"},
			Message:   msg,
		}
	}

	return makeCaptureDir(t, baselineEntries), makeCaptureDir(t, currentEntries)
}

// makeStableCaptures creates two identical captures.
func makeStableCaptures(t *testing.T) (baselineDir, currentDir string) {
	t.Helper()
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	entries := sampleEntries(base)
	return makeCaptureDir(t, entries), makeCaptureDir(t, entries)
}

func TestRunBaselineDiff_CI_Regression(t *testing.T) {
	baselineDir, currentDir := makeRegressionCaptures(t)
	restore := redirectOutput(t)
	defer restore()

	err := runBaselineDiff(baselineDir, currentDir, false, true, []string{"regression"})
	if err == nil {
		t.Fatal("expected FindingsError for regression verdict")
	}

	var ce *cli.CLIError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CLIError, got %T: %v", err, err)
	}
	if ce.Code != cli.ExitFindings {
		t.Errorf("exit code = %d, want %d", ce.Code, cli.ExitFindings)
	}
}

func TestRunBaselineDiff_CI_Stable(t *testing.T) {
	baselineDir, currentDir := makeStableCaptures(t)
	restore := redirectOutput(t)
	defer restore()

	err := runBaselineDiff(baselineDir, currentDir, false, true, []string{"regression"})
	if err != nil {
		t.Fatalf("expected nil for stable verdict, got: %v", err)
	}
}

func TestRunBaselineDiff_CI_FailOnDifferent(t *testing.T) {
	baselineDir, currentDir := makeRegressionCaptures(t)
	restore := redirectOutput(t)
	defer restore()

	// fail-on includes "regression" — should still fail
	err := runBaselineDiff(baselineDir, currentDir, false, true, []string{"regression", "different"})
	if err == nil {
		t.Fatal("expected FindingsError")
	}

	var ce *cli.CLIError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CLIError, got %T: %v", err, err)
	}
	if ce.Code != cli.ExitFindings {
		t.Errorf("exit code = %d, want %d", ce.Code, cli.ExitFindings)
	}
}

func TestRunBaselineDiff_CI_JSON(t *testing.T) {
	baselineDir, currentDir := makeRegressionCaptures(t)

	out := captureStdout(t, func() {
		_ = runBaselineDiff(baselineDir, currentDir, true, true, []string{"regression"})
	})

	// JSON should still be written even when CI fails
	if out == "" {
		t.Fatal("expected JSON output")
	}
	if len(out) < 10 {
		t.Fatalf("JSON output too short: %s", out)
	}
}

func TestVerdictFails(t *testing.T) {
	tests := []struct {
		verdict string
		failOn  []string
		want    bool
	}{
		{"regression", []string{"regression"}, true},
		{"stable", []string{"regression"}, false},
		{"different", []string{"regression", "different"}, true},
		{"improvement", []string{"regression"}, false},
		{"regression", nil, false},
		{"regression", []string{}, false},
	}
	for _, tt := range tests {
		got := verdictFails(tt.verdict, tt.failOn)
		if got != tt.want {
			t.Errorf("verdictFails(%q, %v) = %v, want %v", tt.verdict, tt.failOn, got, tt.want)
		}
	}
}
