package archive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func TestDiffBasic(t *testing.T) {
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	stop := base.Add(2 * time.Minute)

	dirA := t.TempDir()
	dirB := t.TempDir()

	entriesA := makeEntries(10, base, "web")
	entriesB := makeEntries(20, base, "api")

	// Use different label keys so the diff detects unique labels per side
	setupCaptureWithLabel(t, dirA, base, stop, entriesA, "frontend", "web")
	setupCaptureWithLabel(t, dirB, base, stop, entriesB, "backend", "api")

	result, err := Diff(dirA, dirB)
	if err != nil {
		t.Fatal(err)
	}

	if result.A.Lines != 10 {
		t.Errorf("A.Lines = %d, want 10", result.A.Lines)
	}
	if result.B.Lines != 20 {
		t.Errorf("B.Lines = %d, want 20", result.B.Lines)
	}

	if len(result.LabelsOnlyA) != 1 || result.LabelsOnlyA[0] != "frontend" {
		t.Errorf("LabelsOnlyA = %v, want [frontend]", result.LabelsOnlyA)
	}
	if len(result.LabelsOnlyB) != 1 || result.LabelsOnlyB[0] != "backend" {
		t.Errorf("LabelsOnlyB = %v, want [backend]", result.LabelsOnlyB)
	}
}

func TestDiffSharedLabels(t *testing.T) {
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	stop := base.Add(time.Minute)

	dirA := t.TempDir()
	dirB := t.TempDir()

	entriesA := makeEntries(5, base, "web")
	entriesB := makeEntries(5, base, "web")

	setupCapture(t, dirA, base, stop, entriesA, "web")
	setupCapture(t, dirB, base, stop, entriesB, "web")

	result, err := Diff(dirA, dirB)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.LabelsOnlyA) != 0 {
		t.Errorf("LabelsOnlyA = %v, want empty", result.LabelsOnlyA)
	}
	if len(result.LabelsOnlyB) != 0 {
		t.Errorf("LabelsOnlyB = %v, want empty", result.LabelsOnlyB)
	}
}

func TestDiffErrorPatterns(t *testing.T) {
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	stop := base.Add(time.Minute)

	dirA := t.TempDir()
	dirB := t.TempDir()

	entriesA := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "web"}, Message: "ERROR: connection refused"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "web"}, Message: "ok line"},
	}
	entriesB := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "web"}, Message: "ERROR: timeout expired"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "web"}, Message: "ok line"},
	}

	setupCapture(t, dirA, base, stop, entriesA, "web")
	setupCapture(t, dirB, base, stop, entriesB, "web")

	result, err := Diff(dirA, dirB)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.ErrorsOnlyA) == 0 {
		t.Error("expected ErrorsOnlyA to have entries")
	}
	if len(result.ErrorsOnlyB) == 0 {
		t.Error("expected ErrorsOnlyB to have entries")
	}
}

func TestDiffRateComparison(t *testing.T) {
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	stop := base.Add(3 * time.Minute)

	dirA := t.TempDir()
	dirB := t.TempDir()

	// A has entries across 2 minutes, B has entries across 3 minutes
	entriesA := makeEntries(10, base, "web")
	entriesB := makeEntries(10, base, "web")
	// Add entries in minute 2 for B only
	for i := 0; i < 5; i++ {
		entriesB = append(entriesB, recv.LogEntry{
			Timestamp: base.Add(2*time.Minute + time.Duration(i)*time.Second),
			Labels:    map[string]string{"app": "web"},
			Message:   fmt.Sprintf("extra line %d", i),
		})
	}

	setupCapture(t, dirA, base, stop, entriesA, "web")
	setupCapture(t, dirB, base, stop, entriesB, "web")

	result, err := Diff(dirA, dirB)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.RateCompare) == 0 {
		t.Fatal("expected rate buckets")
	}

	// Verify sorted by time
	for i := 1; i < len(result.RateCompare); i++ {
		if !result.RateCompare[i-1].Minute.Before(result.RateCompare[i].Minute) {
			t.Error("rate buckets not sorted by time")
		}
	}
}

func TestDiffWriteJSON(t *testing.T) {
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	stop := base.Add(time.Minute)

	dirA := t.TempDir()
	dirB := t.TempDir()

	setupCapture(t, dirA, base, stop, makeEntries(5, base, "web"), "web")
	setupCapture(t, dirB, base, stop, makeEntries(5, base, "api"), "api")

	result, err := Diff(dirA, dirB)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := result.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}

	var decoded DiffResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if decoded.A.Lines != result.A.Lines {
		t.Errorf("decoded A.Lines = %d, want %d", decoded.A.Lines, result.A.Lines)
	}
}

func TestDiffInvalidDir(t *testing.T) {
	_, err := Diff("/nonexistent/a", "/nonexistent/b")
	if err == nil {
		t.Fatal("expected error for invalid directory")
	}
}

func TestDiffEmptyCapture(t *testing.T) {
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	stop := base.Add(time.Minute)

	dirA := t.TempDir()
	dirB := t.TempDir()

	// No data files, just metadata and empty index
	writeMetadata(t, dirA, base, stop, 0)
	writeIndex(t, dirA, nil)
	writeMetadata(t, dirB, base, stop, 0)
	writeIndex(t, dirB, nil)

	result, err := Diff(dirA, dirB)
	if err != nil {
		t.Fatal(err)
	}

	if result.A.Lines != 0 || result.B.Lines != 0 {
		t.Errorf("expected 0 lines, got A=%d B=%d", result.A.Lines, result.B.Lines)
	}
	if len(result.RateCompare) != 0 {
		t.Errorf("expected no rate buckets, got %d", len(result.RateCompare))
	}
}

func TestDiff_OneSideEmpty(t *testing.T) {
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	stop := base.Add(time.Minute)

	dirA := t.TempDir()
	dirB := t.TempDir()

	// A has data, B is an empty capture (metadata + empty index only)
	entriesA := makeEntries(15, base, "web")
	setupCapture(t, dirA, base, stop, entriesA, "web")

	writeMetadata(t, dirB, base, stop, 0)
	writeIndex(t, dirB, nil)

	result, err := Diff(dirA, dirB)
	if err != nil {
		t.Fatal(err)
	}

	if result.A.Lines != 15 {
		t.Errorf("A.Lines = %d, want 15", result.A.Lines)
	}
	if result.B.Lines != 0 {
		t.Errorf("B.Lines = %d, want 0", result.B.Lines)
	}

	// A has a label, B has none
	if len(result.LabelsOnlyA) != 1 || result.LabelsOnlyA[0] != "app" {
		t.Errorf("LabelsOnlyA = %v, want [app]", result.LabelsOnlyA)
	}
	if len(result.LabelsOnlyB) != 0 {
		t.Errorf("LabelsOnlyB = %v, want empty", result.LabelsOnlyB)
	}

	// Rate comparison should have buckets only for A
	for _, b := range result.RateCompare {
		if b.RateB != 0 {
			t.Errorf("RateB should be 0 for empty capture, got %d at %s", b.RateB, b.Minute)
		}
	}

	// Now test the reverse: A is empty, B has data
	dirC := t.TempDir()
	dirD := t.TempDir()

	writeMetadata(t, dirC, base, stop, 0)
	writeIndex(t, dirC, nil)

	entriesD := makeEntries(10, base, "api")
	setupCapture(t, dirD, base, stop, entriesD, "api")

	result2, err := Diff(dirC, dirD)
	if err != nil {
		t.Fatal(err)
	}

	if result2.A.Lines != 0 {
		t.Errorf("A.Lines = %d, want 0", result2.A.Lines)
	}
	if result2.B.Lines != 10 {
		t.Errorf("B.Lines = %d, want 10", result2.B.Lines)
	}
	if len(result2.LabelsOnlyA) != 0 {
		t.Errorf("LabelsOnlyA = %v, want empty", result2.LabelsOnlyA)
	}
	if len(result2.LabelsOnlyB) != 1 || result2.LabelsOnlyB[0] != "app" {
		t.Errorf("LabelsOnlyB = %v, want [app]", result2.LabelsOnlyB)
	}
}

// setupCapture creates a minimal capture directory with metadata, index, and one data file.
func setupCapture(t *testing.T, dir string, started, stopped time.Time, entries []recv.LogEntry, label string) {
	t.Helper()
	setupCaptureWithLabel(t, dir, started, stopped, entries, "app", label)
}

// setupCaptureWithLabel creates a capture directory with a custom label key.
func setupCaptureWithLabel(t *testing.T, dir string, started, stopped time.Time, entries []recv.LogEntry, labelKey, labelVal string) {
	t.Helper()
	writeMetadata(t, dir, started, stopped, int64(len(entries)))
	writeIndex(t, dir, []rotate.IndexEntry{
		{
			File:  "data-001.jsonl",
			From:  started,
			To:    stopped,
			Lines: int64(len(entries)),
			Bytes: 1024,
			Labels: map[string]map[string]int64{
				labelKey: {labelVal: int64(len(entries))},
			},
		},
	})
	writeDataFile(t, dir, "data-001.jsonl", entries)
}

func TestBaselineDiff_Regression(t *testing.T) {
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	stop := base.Add(time.Minute)

	baselineDir := t.TempDir()
	currentDir := t.TempDir()

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

	setupCapture(t, baselineDir, base, stop, baselineEntries, "web")
	setupCapture(t, currentDir, base, stop, currentEntries, "web")

	result, err := BaselineDiff(baselineDir, currentDir)
	if err != nil {
		t.Fatal(err)
	}

	if result.Verdict != "regression" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "regression")
	}
	if result.Confidence < 0.5 {
		t.Errorf("Confidence = %.2f, want >= 0.5", result.Confidence)
	}
	if len(result.NewErrorPatterns) == 0 {
		t.Error("expected new error patterns")
	}

	// Check that the FATAL pattern shows up as new
	foundFatal := false
	for _, p := range result.NewErrorPatterns {
		if p.BaselineCount == 0 && p.Count > 0 {
			foundFatal = true
		}
	}
	if !foundFatal {
		t.Error("expected at least one entirely new error pattern")
	}

	// Verify JSON round-trip
	var buf bytes.Buffer
	if err := result.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var decoded BaselineDiffResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded.Verdict != "regression" {
		t.Errorf("decoded Verdict = %q, want %q", decoded.Verdict, "regression")
	}
}

func TestBaselineDiff_Stable(t *testing.T) {
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	stop := base.Add(time.Minute)

	baselineDir := t.TempDir()
	currentDir := t.TempDir()

	// Both captures: 20 lines, 1 error each with the same pattern
	makeStableEntries := func() []recv.LogEntry {
		entries := make([]recv.LogEntry, 20)
		for i := range entries {
			msg := fmt.Sprintf("normal line %d", i)
			if i == 0 {
				msg = "ERROR: connection refused"
			}
			entries[i] = recv.LogEntry{
				Timestamp: base.Add(time.Duration(i) * time.Second),
				Labels:    map[string]string{"app": "web"},
				Message:   msg,
			}
		}
		return entries
	}

	setupCapture(t, baselineDir, base, stop, makeStableEntries(), "web")
	setupCapture(t, currentDir, base, stop, makeStableEntries(), "web")

	result, err := BaselineDiff(baselineDir, currentDir)
	if err != nil {
		t.Fatal(err)
	}

	if result.Verdict != "stable" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "stable")
	}
	if result.Confidence < 0.7 {
		t.Errorf("Confidence = %.2f, want >= 0.7", result.Confidence)
	}
	if len(result.NewErrorPatterns) != 0 {
		t.Errorf("NewErrorPatterns = %d, want 0", len(result.NewErrorPatterns))
	}
	if len(result.MissingLabels) != 0 {
		t.Errorf("MissingLabels = %v, want empty", result.MissingLabels)
	}
	if len(result.NewLabels) != 0 {
		t.Errorf("NewLabels = %v, want empty", result.NewLabels)
	}
}

func TestBaselineDiff_NewPatterns(t *testing.T) {
	base := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	stop := base.Add(time.Minute)

	baselineDir := t.TempDir()
	currentDir := t.TempDir()

	// Baseline: 20 lines, no errors
	baselineEntries := makeEntries(20, base, "web")

	// Current: 20 lines, 5 new error patterns that didn't exist in baseline
	currentEntries := make([]recv.LogEntry, 20)
	for i := range currentEntries {
		msg := fmt.Sprintf("normal line %d", i)
		if i < 3 {
			msg = "ERROR: database connection timeout"
		}
		if i >= 3 && i < 5 {
			msg = "PANIC: nil pointer dereference"
		}
		currentEntries[i] = recv.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Labels:    map[string]string{"app": "web", "env": "staging"},
			Message:   msg,
		}
	}

	setupCapture(t, baselineDir, base, stop, baselineEntries, "web")
	setupCaptureWithLabel(t, currentDir, base, stop, currentEntries, "app", "web")

	result, err := BaselineDiff(baselineDir, currentDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.NewErrorPatterns) < 2 {
		t.Errorf("NewErrorPatterns = %d, want >= 2", len(result.NewErrorPatterns))
	}

	// All new patterns should have BaselineCount == 0
	for _, p := range result.NewErrorPatterns {
		if p.BaselineCount != 0 {
			t.Errorf("pattern %q BaselineCount = %d, want 0", p.Pattern, p.BaselineCount)
		}
		if p.Count == 0 {
			t.Errorf("pattern %q Count = 0, want > 0", p.Pattern)
		}
	}

	// Verify verdict is regression (error rate went from 0 to 25%, new patterns)
	if result.Verdict != "regression" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "regression")
	}

	// Verify text output doesn't panic
	var buf bytes.Buffer
	result.WriteText(&buf)
	if buf.Len() == 0 {
		t.Error("WriteText produced empty output")
	}
}
