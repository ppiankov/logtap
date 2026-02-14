package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func TestInspectNormalCapture(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 32, 0, 0, time.UTC)
	stop := base.Add(2*time.Hour + 13*time.Minute)

	writeMetadata(t, dir, base, stop, 1000)
	writeIndex(t, dir, []rotate.IndexEntry{
		{
			File:  "2024-01-15T103200-000.jsonl.zst",
			From:  base,
			To:    base.Add(30 * time.Minute),
			Lines: 600,
			Bytes: 30000,
			Labels: map[string]map[string]int64{
				"app":       {"api-service": 400, "worker": 200},
				"namespace": {"default": 600},
			},
		},
		{
			File:  "2024-01-15T110200-000.jsonl.zst",
			From:  base.Add(30 * time.Minute),
			To:    stop,
			Lines: 400,
			Bytes: 20000,
			Labels: map[string]map[string]int64{
				"app":       {"api-service": 300, "worker": 100},
				"namespace": {"default": 400},
			},
		},
	})

	// create dummy data files for disk stats
	writeFile(t, filepath.Join(dir, "2024-01-15T103200-000.jsonl.zst"), 15000)
	writeFile(t, filepath.Join(dir, "2024-01-15T110200-000.jsonl.zst"), 10000)

	s, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}

	if s.TotalLines != 1000 {
		t.Errorf("TotalLines = %d, want 1000", s.TotalLines)
	}
	if s.TotalBytes != 50000 {
		t.Errorf("TotalBytes = %d, want 50000", s.TotalBytes)
	}
	if s.Files != 2 {
		t.Errorf("Files = %d, want 2", s.Files)
	}
	if s.DiskSize != 25000 {
		t.Errorf("DiskSize = %d, want 25000", s.DiskSize)
	}

	// check label aggregation
	appLabels := s.Labels["app"]
	if len(appLabels) != 2 {
		t.Fatalf("app labels count = %d, want 2", len(appLabels))
	}
	// sorted by lines descending: api-service(700) > worker(300)
	if appLabels[0].Value != "api-service" || appLabels[0].Lines != 700 {
		t.Errorf("app[0] = {%s, %d}, want {api-service, 700}", appLabels[0].Value, appLabels[0].Lines)
	}
	if appLabels[1].Value != "worker" || appLabels[1].Lines != 300 {
		t.Errorf("app[1] = {%s, %d}, want {worker, 300}", appLabels[1].Value, appLabels[1].Lines)
	}

	nsLabels := s.Labels["namespace"]
	if len(nsLabels) != 1 || nsLabels[0].Lines != 1000 {
		t.Errorf("namespace labels = %v, want [{default, 1000}]", nsLabels)
	}

	// timeline should have buckets
	if len(s.Timeline) == 0 {
		t.Error("expected non-empty timeline")
	}
}

func TestInspectEmptyCapture(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	writeMetadata(t, dir, base, base, 0)

	s, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}

	if s.TotalLines != 0 {
		t.Errorf("TotalLines = %d, want 0", s.TotalLines)
	}
	if s.Files != 0 {
		t.Errorf("Files = %d, want 0", s.Files)
	}
	if len(s.Labels) != 0 {
		t.Errorf("Labels = %v, want empty", s.Labels)
	}
	if len(s.Timeline) != 0 {
		t.Errorf("Timeline = %v, want empty", s.Timeline)
	}
}

func TestInspectSingleFile(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	writeMetadata(t, dir, base, base.Add(5*time.Minute), 500)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl.zst",
		From:  base,
		To:    base.Add(5 * time.Minute),
		Lines: 500,
		Bytes: 25000,
		Labels: map[string]map[string]int64{
			"app": {"gateway": 500},
		},
	}})
	writeFile(t, filepath.Join(dir, "2024-01-15T100000-000.jsonl.zst"), 12000)

	s, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}

	if s.TotalLines != 500 {
		t.Errorf("TotalLines = %d, want 500", s.TotalLines)
	}
	if s.Files != 1 {
		t.Errorf("Files = %d, want 1", s.Files)
	}
	if len(s.Labels["app"]) != 1 || s.Labels["app"][0].Value != "gateway" {
		t.Errorf("unexpected labels: %v", s.Labels)
	}
}

func TestInspectTimelineBucketDistribution(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	writeMetadata(t, dir, base, base.Add(5*time.Minute), 300)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(4 * time.Minute),
		Lines: 300,
		Bytes: 15000,
	}})
	writeFile(t, filepath.Join(dir, "2024-01-15T100000-000.jsonl"), 15000)

	s, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 5 minute buckets (10:00 through 10:04)
	if len(s.Timeline) != 5 {
		t.Fatalf("timeline buckets = %d, want 5", len(s.Timeline))
	}

	// lines distributed evenly: 300 / 5 = 60 per bucket
	var totalBucketLines int64
	for _, b := range s.Timeline {
		totalBucketLines += b.Lines
	}
	if totalBucketLines != 300 {
		t.Errorf("total bucket lines = %d, want 300", totalBucketLines)
	}

	// each bucket should get 60 lines
	for i, b := range s.Timeline {
		if b.Lines != 60 {
			t.Errorf("bucket[%d] lines = %d, want 60", i, b.Lines)
		}
	}
}

func TestInspectTimelineSingleMinute(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 30, 0, time.UTC) // mid-minute

	writeMetadata(t, dir, base, base.Add(20*time.Second), 100)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(20 * time.Second),
		Lines: 100,
		Bytes: 5000,
	}})
	writeFile(t, filepath.Join(dir, "2024-01-15T100000-000.jsonl"), 5000)

	s, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}

	// both from and to truncate to same minute -> 1 bucket
	if len(s.Timeline) != 1 {
		t.Fatalf("timeline buckets = %d, want 1", len(s.Timeline))
	}
	if s.Timeline[0].Lines != 100 {
		t.Errorf("bucket lines = %d, want 100", s.Timeline[0].Lines)
	}
}

func TestInspectMissingMetadata(t *testing.T) {
	dir := t.TempDir()
	_, err := Inspect(dir)
	if err == nil {
		t.Error("expected error for missing metadata")
	}
}

func TestInspectNoIndex(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	writeMetadata(t, dir, base, base, 0)
	// no index.jsonl

	s, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalLines != 0 {
		t.Errorf("TotalLines = %d, want 0", s.TotalLines)
	}
}

func TestWriteText(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 32, 0, 0, time.UTC)
	stop := base.Add(2*time.Hour + 13*time.Minute)

	s := &Summary{
		Dir: "./capture",
		Meta: &recv.Metadata{
			Version: 1,
			Format:  "jsonl+zstd",
			Started: base,
			Stopped: stop,
		},
		Files:      23,
		TotalLines: 14832901,
		TotalBytes: 9 * (1 << 30),
		DiskSize:   8*1<<30 + 400*(1<<20),
		Labels: map[string][]LabelVal{
			"app": {
				{Value: "api-service", Lines: 8412001, Bytes: 4*1<<30 + 200*(1<<20)},
				{Value: "worker", Lines: 4102300, Bytes: 2*1<<30 + 800*(1<<20)},
			},
		},
		Timeline: []Bucket{
			{Time: base, Lines: 100},
			{Time: base.Add(time.Minute), Lines: 200},
			{Time: base.Add(2 * time.Minute), Lines: 150},
		},
	}

	var buf bytes.Buffer
	s.WriteText(&buf)
	out := buf.String()

	// verify key sections present
	checks := []string{
		"Capture: ./capture",
		"Format:  jsonl+zstd (v1)",
		"Period:",
		"2h 13m",
		"Size:",
		"23 files",
		"Lines:   14,832,901",
		"Labels:",
		"app:",
		"api-service",
		"worker",
		"Timeline (1-min buckets):",
	}
	for _, check := range checks {
		if !strings.Contains(out, check) {
			t.Errorf("text output missing %q\noutput:\n%s", check, out)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	s := &Summary{
		Dir: "./test",
		Meta: &recv.Metadata{
			Version: 1,
			Format:  "jsonl",
			Started: base,
		},
		Files:      1,
		TotalLines: 100,
		TotalBytes: 5000,
		DiskSize:   3000,
	}

	var buf bytes.Buffer
	if err := s.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}

	// verify it's valid JSON
	var parsed Summary
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput: %s", err, buf.String())
	}
	if parsed.TotalLines != 100 {
		t.Errorf("JSON total_lines = %d, want 100", parsed.TotalLines)
	}
	if parsed.Dir != "./test" {
		t.Errorf("JSON dir = %q, want %q", parsed.Dir, "./test")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{9 * 1073741824, "9.0 GB"},
	}
	for _, tt := range tests {
		got := FormatBytes(tt.input)
		if got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{100, "100"},
		{1000, "1,000"},
		{1000000, "1,000,000"},
		{14832901, "14,832,901"},
	}
	for _, tt := range tests {
		got := FormatCount(tt.input)
		if got != tt.want {
			t.Errorf("FormatCount(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatHumanDuration(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{30 * time.Second, "30s"},
		{5*time.Minute + 30*time.Second, "5m 30s"},
		{2*time.Hour + 13*time.Minute, "2h 13m"},
	}
	for _, tt := range tests {
		got := formatHumanDuration(tt.input)
		if got != tt.want {
			t.Errorf("formatHumanDuration(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// writeFile creates a file with the given size filled with zeros.
func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	data := make([]byte, size)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
