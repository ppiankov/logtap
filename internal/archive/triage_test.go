package archive

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func setupTriageSource(t *testing.T) (string, time.Time) {
	t.Helper()
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "request started"},
		{Timestamp: base.Add(1 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "connection refused to payments:8080"},
		{Timestamp: base.Add(2 * time.Minute), Labels: map[string]string{"app": "worker"}, Message: "job completed"},
		{Timestamp: base.Add(3 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "5xx server error status=503"},
		{Timestamp: base.Add(3 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "connection refused to payments:8080"},
		{Timestamp: base.Add(4 * time.Minute), Labels: map[string]string{"app": "gateway"}, Message: "request forwarded"},
		{Timestamp: base.Add(5 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "panic: nil pointer dereference"},
		{Timestamp: base.Add(6 * time.Minute), Labels: map[string]string{"app": "worker"}, Message: "timeout processing job=12345"},
		{Timestamp: base.Add(7 * time.Minute), Labels: map[string]string{"app": "gateway"}, Message: "health check ok"},
		{Timestamp: base.Add(8 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "connection refused to payments:8080"},
	}

	writeMetadata(t, dir, base, base.Add(8*time.Minute), 10)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(8 * time.Minute),
		Lines: 10,
		Bytes: 1000,
		Labels: map[string]map[string]int64{
			"app": {"api": 6, "worker": 2, "gateway": 2},
		},
	}})

	return dir, base
}

func TestTriageBasic(t *testing.T) {
	src, _ := setupTriageSource(t)

	result, err := Triage(src, TriageConfig{Jobs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalLines != 10 {
		t.Errorf("TotalLines = %d, want 10", result.TotalLines)
	}
	if result.ErrorLines == 0 {
		t.Error("ErrorLines = 0, want > 0")
	}
	if len(result.Timeline) == 0 {
		t.Error("Timeline is empty")
	}
	if len(result.Errors) == 0 {
		t.Error("Errors is empty")
	}
	if len(result.Talkers) == 0 {
		t.Error("Talkers is empty")
	}
}

func TestTriageErrorDetection(t *testing.T) {
	src, _ := setupTriageSource(t)

	result, err := Triage(src, TriageConfig{Jobs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// errors: "connection refused" x3, "5xx server error" x1, "panic" x1, "timeout" x1 = 6
	if result.ErrorLines != 6 {
		t.Errorf("ErrorLines = %d, want 6", result.ErrorLines)
	}
}

func TestTriageSignatureNormalization(t *testing.T) {
	src, _ := setupTriageSource(t)

	result, err := Triage(src, TriageConfig{Jobs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// "connection refused to payments:8080" appears 3x, should be grouped
	var found bool
	for _, e := range result.Errors {
		if strings.Contains(e.Signature, "connection refused") {
			if e.Count != 3 {
				t.Errorf("refused count = %d, want 3", e.Count)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("connection refused signature not found in errors")
	}
}

func TestTriageTopTalkers(t *testing.T) {
	src, _ := setupTriageSource(t)

	result, err := Triage(src, TriageConfig{Jobs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	appTalkers := result.Talkers["app"]
	if len(appTalkers) == 0 {
		t.Fatal("no app talkers")
	}

	// api has most lines (6), should be first
	if appTalkers[0].Value != "api" {
		t.Errorf("top talker = %q, want %q", appTalkers[0].Value, "api")
	}
	if appTalkers[0].TotalLines != 6 {
		t.Errorf("api lines = %d, want 6", appTalkers[0].TotalLines)
	}
	if appTalkers[0].ErrorLines != 5 {
		t.Errorf("api errors = %d, want 5", appTalkers[0].ErrorLines)
	}
}

func TestTriageTimeline(t *testing.T) {
	src, _ := setupTriageSource(t)

	result, err := Triage(src, TriageConfig{Jobs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 9 minutes (10:00 through 10:08)
	if len(result.Timeline) != 9 {
		t.Errorf("timeline buckets = %d, want 9", len(result.Timeline))
	}

	// total lines across timeline should sum to 10
	var total int64
	for _, b := range result.Timeline {
		total += b.TotalLines
	}
	if total != 10 {
		t.Errorf("timeline total = %d, want 10", total)
	}
}

func TestTriageWindows(t *testing.T) {
	src, _ := setupTriageSource(t)

	result, err := Triage(src, TriageConfig{Jobs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// should have peak error window
	if result.Windows.PeakError == nil {
		t.Error("PeakError window is nil")
	}

	// should have incident start window
	if result.Windows.IncidentStart == nil {
		t.Error("IncidentStart window is nil")
	}
}

func TestTriageProgress(t *testing.T) {
	src, _ := setupTriageSource(t)

	var calls []TriageProgress
	_, err := Triage(src, TriageConfig{Jobs: 1}, func(p TriageProgress) {
		calls = append(calls, p)
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(calls) == 0 {
		t.Error("progress callback never called")
	}

	last := calls[len(calls)-1]
	if last.Scanned != 10 {
		t.Errorf("final scanned = %d, want 10", last.Scanned)
	}
}

func TestTriageEmptyCapture(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	writeMetadata(t, dir, base, base, 0)

	result, err := Triage(dir, TriageConfig{Jobs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalLines != 0 {
		t.Errorf("TotalLines = %d, want 0", result.TotalLines)
	}
	if result.ErrorLines != 0 {
		t.Errorf("ErrorLines = %d, want 0", result.ErrorLines)
	}
	if len(result.Timeline) != 0 {
		t.Errorf("Timeline = %d buckets, want 0", len(result.Timeline))
	}
}

func TestTriageOutputFiles(t *testing.T) {
	src, _ := setupTriageSource(t)
	outDir := t.TempDir()

	result, err := Triage(src, TriageConfig{Jobs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// write all output files
	files := map[string]func(f *os.File){
		"summary.md": func(f *os.File) {
			result.WriteSummary(f)
		},
		"timeline.csv": func(f *os.File) {
			result.WriteTimeline(f)
		},
		"top_errors.txt": func(f *os.File) {
			result.WriteTopErrors(f)
		},
		"top_talkers.txt": func(f *os.File) {
			result.WriteTopTalkers(f)
		},
		"windows.json": func(f *os.File) {
			_ = result.WriteWindows(f)
		},
	}

	for name, writeFn := range files {
		path := filepath.Join(outDir, name)
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		writeFn(f)
		_ = f.Close()

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}

	// verify timeline.csv is parseable
	csvData, _ := os.ReadFile(filepath.Join(outDir, "timeline.csv"))
	csvReader := csv.NewReader(bytes.NewReader(csvData))
	records, err := csvReader.ReadAll()
	if err != nil {
		t.Fatalf("parse timeline.csv: %v", err)
	}
	if len(records) < 2 { // header + at least 1 row
		t.Errorf("timeline.csv has %d records, want >= 2", len(records))
	}
	if records[0][0] != "minute" {
		t.Errorf("timeline.csv header = %v, want [minute ...]", records[0])
	}

	// verify windows.json is parseable
	windowsData, _ := os.ReadFile(filepath.Join(outDir, "windows.json"))
	var w TriageWindows
	if err := json.Unmarshal(windowsData, &w); err != nil {
		t.Fatalf("parse windows.json: %v", err)
	}
}

func TestTriageMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	file1 := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "request started"},
		{Timestamp: base.Add(1 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "timeout error"},
	}
	file2 := []recv.LogEntry{
		{Timestamp: base.Add(5 * time.Minute), Labels: map[string]string{"app": "worker"}, Message: "job failed with error"},
		{Timestamp: base.Add(6 * time.Minute), Labels: map[string]string{"app": "worker"}, Message: "recovery complete"},
	}

	writeMetadata(t, dir, base, base.Add(6*time.Minute), 4)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", file1)
	writeDataFile(t, dir, "2024-01-15T100500-000.jsonl", file2)
	writeIndex(t, dir, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(1 * time.Minute), Lines: 2, Bytes: 200},
		{File: "2024-01-15T100500-000.jsonl", From: base.Add(5 * time.Minute), To: base.Add(6 * time.Minute), Lines: 2, Bytes: 200},
	})

	result, err := Triage(dir, TriageConfig{Jobs: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalLines != 4 {
		t.Errorf("TotalLines = %d, want 4", result.TotalLines)
	}
	// "timeout error" + "job failed with error" = 2 errors
	if result.ErrorLines != 2 {
		t.Errorf("ErrorLines = %d, want 2", result.ErrorLines)
	}
}

func TestTriageSkipsRotatedFile(t *testing.T) {
	src, _ := setupTriageSource(t)

	// Delete the data file to simulate rotation during scan.
	dataFile := filepath.Join(src, "2024-01-15T100000-000.jsonl")
	if err := os.Remove(dataFile); err != nil {
		t.Fatal(err)
	}

	// Triage should succeed despite the missing file.
	result, err := Triage(src, TriageConfig{Jobs: 1}, nil)
	if err != nil {
		t.Fatalf("triage should not fail on rotated file: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestTriageSummaryContainsKey(t *testing.T) {
	src, _ := setupTriageSource(t)

	result, err := Triage(src, TriageConfig{Jobs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	result.WriteSummary(&buf)
	summary := buf.String()

	for _, want := range []string{"# Triage:", "## Top Errors", "## Top Talkers"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q", want)
		}
	}
}
