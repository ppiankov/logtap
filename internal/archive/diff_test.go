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
