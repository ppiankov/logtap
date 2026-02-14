package archive

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func setupExportSource(t *testing.T) (string, time.Time) {
	t.Helper()
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "request started"},
		{Timestamp: base.Add(1 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "timeout error"},
		{Timestamp: base.Add(2 * time.Minute), Labels: map[string]string{"app": "worker"}, Message: "job completed"},
		{Timestamp: base.Add(3 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "5xx server error"},
		{Timestamp: base.Add(4 * time.Minute), Labels: map[string]string{"app": "gateway"}, Message: "request forwarded"},
	}

	writeMetadata(t, dir, base, base.Add(4*time.Minute), 5)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(4 * time.Minute),
		Lines: 5,
		Bytes: 500,
		Labels: map[string]map[string]int64{
			"app": {"api": 3, "worker": 1, "gateway": 1},
		},
	}})

	return dir, base
}

func TestExportParquet(t *testing.T) {
	src, _ := setupExportSource(t)
	out := filepath.Join(t.TempDir(), "out.parquet")

	err := Export(src, out, FormatParquet, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// verify file exists and is non-empty
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("parquet file is empty")
	}

	// read back and verify row count
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	stat, _ := f.Stat()
	pf, err := parquet.OpenFile(f, stat.Size())
	if err != nil {
		t.Fatal(err)
	}
	if pf.NumRows() != 5 {
		t.Errorf("parquet rows = %d, want 5", pf.NumRows())
	}
}

func TestExportCSV(t *testing.T) {
	src, _ := setupExportSource(t)
	out := filepath.Join(t.TempDir(), "out.csv")

	err := Export(src, out, FormatCSV, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	// header + 5 rows
	if len(records) != 6 {
		t.Fatalf("CSV records = %d, want 6 (1 header + 5 data)", len(records))
	}
	if records[0][0] != "ts" || records[0][1] != "labels" || records[0][2] != "msg" {
		t.Errorf("CSV header = %v, want [ts labels msg]", records[0])
	}
}

func TestExportJSONL(t *testing.T) {
	src, _ := setupExportSource(t)
	out := filepath.Join(t.TempDir(), "out.jsonl")

	err := Export(src, out, FormatJSONL, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry recv.LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid JSON on line %d: %v", count+1, err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("JSONL lines = %d, want 5", count)
	}
}

func TestExportWithFilter(t *testing.T) {
	src, _ := setupExportSource(t)
	out := filepath.Join(t.TempDir(), "filtered.jsonl")

	filter := &Filter{
		Labels: []LabelMatcher{{Key: "app", Value: "api"}},
	}

	err := Export(src, out, FormatJSONL, filter, nil)
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if scanner.Text() != "" {
			count++
		}
	}
	if count != 3 {
		t.Errorf("filtered JSONL lines = %d, want 3", count)
	}
}

func TestExportEmptyResult(t *testing.T) {
	src, _ := setupExportSource(t)
	out := filepath.Join(t.TempDir(), "empty.jsonl")

	filter := &Filter{
		Grep: regexp.MustCompile(`nonexistent_pattern_xyz`),
	}

	err := Export(src, out, FormatJSONL, filter, nil)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Errorf("empty export file size = %d, want 0", info.Size())
	}
}

func TestExportProgress(t *testing.T) {
	src, _ := setupExportSource(t)
	out := filepath.Join(t.TempDir(), "out.jsonl")

	var calls []ExportProgress
	err := Export(src, out, FormatJSONL, nil, func(p ExportProgress) {
		calls = append(calls, p)
	})
	if err != nil {
		t.Fatal(err)
	}

	// should get at least the final progress call
	if len(calls) == 0 {
		t.Error("progress callback never called")
	}

	last := calls[len(calls)-1]
	if last.Written != 5 {
		t.Errorf("final written = %d, want 5", last.Written)
	}
	if last.Total != 5 {
		t.Errorf("final total = %d, want 5", last.Total)
	}
}

func TestExportCSVLabelFormat(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := []recv.LogEntry{
		{
			Timestamp: base,
			Labels:    map[string]string{"env": "prod", "app": "api", "region": "us-east"},
			Message:   "test",
		},
	}

	writeMetadata(t, dir, base, base, 1)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base,
		Lines: 1,
		Bytes: 100,
	}})

	out := filepath.Join(t.TempDir(), "labels.csv")
	err := Export(dir, out, FormatCSV, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 2 {
		t.Fatalf("CSV records = %d, want 2", len(records))
	}

	// labels should be sorted alphabetically: app=api;env=prod;region=us-east
	labels := records[1][1]
	want := "app=api;env=prod;region=us-east"
	if labels != want {
		t.Errorf("labels = %q, want %q", labels, want)
	}
}

func TestExportParquetWithFilter(t *testing.T) {
	src, base := setupExportSource(t)
	out := filepath.Join(t.TempDir(), "filtered.parquet")

	filter := &Filter{
		From: base.Add(1 * time.Minute),
		To:   base.Add(3 * time.Minute),
	}

	err := Export(src, out, FormatParquet, filter, nil)
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	stat, _ := f.Stat()
	pf, err := parquet.OpenFile(f, stat.Size())
	if err != nil {
		t.Fatal(err)
	}
	if pf.NumRows() != 3 {
		t.Errorf("parquet rows = %d, want 3", pf.NumRows())
	}
}

func TestFlattenLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"nil", nil, ""},
		{"empty", map[string]string{}, ""},
		{"single", map[string]string{"app": "api"}, "app=api"},
		{"multiple sorted", map[string]string{"env": "prod", "app": "api"}, "app=api;env=prod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := flattenLabels(tt.labels)
			if got != tt.want {
				t.Errorf("flattenLabels() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExportCSVTimestampFormat(t *testing.T) {
	src, base := setupExportSource(t)
	out := filepath.Join(t.TempDir(), "ts.csv")

	err := Export(src, out, FormatCSV, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	// first data row timestamp should parse as RFC3339Nano
	ts := records[1][0]
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("timestamp %q is not valid RFC3339Nano: %v", ts, err)
	}
	if !parsed.Equal(base) {
		t.Errorf("parsed timestamp = %v, want %v", parsed, base)
	}
}

func TestExportCSVGrepFilter(t *testing.T) {
	src, _ := setupExportSource(t)
	out := filepath.Join(t.TempDir(), "grep.csv")

	filter := &Filter{
		Grep: regexp.MustCompile(`5xx`),
	}

	err := Export(src, out, FormatCSV, filter, nil)
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	// header + 1 matching row ("5xx server error")
	if len(records) != 2 {
		t.Fatalf("CSV records = %d, want 2 (1 header + 1 data)", len(records))
	}
	if !strings.Contains(records[1][2], "5xx") {
		t.Errorf("message = %q, should contain '5xx'", records[1][2])
	}
}
