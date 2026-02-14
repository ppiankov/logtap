package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func setupSliceSource(t *testing.T) (string, time.Time) {
	t.Helper()
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "request started"},
		{Timestamp: base.Add(1 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "timeout error"},
		{Timestamp: base.Add(2 * time.Minute), Labels: map[string]string{"app": "worker"}, Message: "job completed"},
		{Timestamp: base.Add(3 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "5xx server error"},
		{Timestamp: base.Add(4 * time.Minute), Labels: map[string]string{"app": "gateway"}, Message: "request forwarded"},
		{Timestamp: base.Add(5 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "request completed"},
		{Timestamp: base.Add(6 * time.Minute), Labels: map[string]string{"app": "worker"}, Message: "timeout in processing"},
		{Timestamp: base.Add(7 * time.Minute), Labels: map[string]string{"app": "gateway"}, Message: "health check ok"},
		{Timestamp: base.Add(8 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "5xx gateway timeout"},
		{Timestamp: base.Add(9 * time.Minute), Labels: map[string]string{"app": "worker"}, Message: "job started"},
	}

	writeMetadata(t, dir, base, base.Add(9*time.Minute), 10)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(9 * time.Minute),
		Lines: 10,
		Bytes: 1000,
		Labels: map[string]map[string]int64{
			"app": {"api": 5, "worker": 3, "gateway": 2},
		},
	}})

	return dir, base
}

func readSlicedEntries(t *testing.T, dir string) []recv.LogEntry {
	t.Helper()
	reader, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	var entries []recv.LogEntry
	_, err = reader.Scan(nil, func(e recv.LogEntry) bool {
		entries = append(entries, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestSliceTimeRange(t *testing.T) {
	src, base := setupSliceSource(t)
	dst := t.TempDir()

	filter := &Filter{
		From: base.Add(2 * time.Minute),
		To:   base.Add(5 * time.Minute),
	}

	err := Slice(src, dst, filter, SliceConfig{Compress: false}, nil)
	if err != nil {
		t.Fatal(err)
	}

	entries := readSlicedEntries(t, dst)
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4", len(entries))
	}
	if entries[0].Message != "job completed" {
		t.Errorf("first = %q, want %q", entries[0].Message, "job completed")
	}
	if entries[3].Message != "request completed" {
		t.Errorf("last = %q, want %q", entries[3].Message, "request completed")
	}
}

func TestSliceLabelFilter(t *testing.T) {
	src, _ := setupSliceSource(t)
	dst := t.TempDir()

	filter := &Filter{
		Labels: []LabelMatcher{{Key: "app", Value: "worker"}},
	}

	err := Slice(src, dst, filter, SliceConfig{Compress: false}, nil)
	if err != nil {
		t.Fatal(err)
	}

	entries := readSlicedEntries(t, dst)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	for _, e := range entries {
		if e.Labels["app"] != "worker" {
			t.Errorf("entry label app = %q, want %q", e.Labels["app"], "worker")
		}
	}
}

func TestSliceGrepFilter(t *testing.T) {
	src, _ := setupSliceSource(t)
	dst := t.TempDir()

	filter := &Filter{
		Grep: regexp.MustCompile(`timeout|5xx`),
	}

	err := Slice(src, dst, filter, SliceConfig{Compress: false}, nil)
	if err != nil {
		t.Fatal(err)
	}

	entries := readSlicedEntries(t, dst)
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4", len(entries))
	}
	// "timeout error", "5xx server error", "timeout in processing", "5xx gateway timeout"
	re := regexp.MustCompile(`timeout|5xx`)
	for _, e := range entries {
		if !re.MatchString(e.Message) {
			t.Errorf("entry %q should not have matched", e.Message)
		}
	}
}

func TestSliceCombinedFilters(t *testing.T) {
	src, base := setupSliceSource(t)
	dst := t.TempDir()

	filter := &Filter{
		From:   base,
		To:     base.Add(5 * time.Minute),
		Labels: []LabelMatcher{{Key: "app", Value: "api"}},
		Grep:   regexp.MustCompile(`5xx`),
	}

	err := Slice(src, dst, filter, SliceConfig{Compress: false}, nil)
	if err != nil {
		t.Fatal(err)
	}

	entries := readSlicedEntries(t, dst)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Message != "5xx server error" {
		t.Errorf("entry = %q, want %q", entries[0].Message, "5xx server error")
	}
}

func TestSliceEmptyResult(t *testing.T) {
	src, _ := setupSliceSource(t)
	dst := t.TempDir()

	filter := &Filter{
		Grep: regexp.MustCompile(`nonexistent_pattern_xyz`),
	}

	err := Slice(src, dst, filter, SliceConfig{Compress: false}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// metadata should exist with zero lines
	meta, err := recv.ReadMetadata(dst)
	if err != nil {
		t.Fatal(err)
	}
	if meta.TotalLines != 0 {
		t.Errorf("TotalLines = %d, want 0", meta.TotalLines)
	}
}

func TestSliceOutputIsValidCapture(t *testing.T) {
	src, _ := setupSliceSource(t)
	dst := t.TempDir()

	filter := &Filter{
		Labels: []LabelMatcher{{Key: "app", Value: "api"}},
	}

	err := Slice(src, dst, filter, SliceConfig{Compress: false}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// metadata.json exists and is valid
	meta, err := recv.ReadMetadata(dst)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Version != 1 {
		t.Errorf("Version = %d, want 1", meta.Version)
	}
	if meta.TotalLines != 5 {
		t.Errorf("TotalLines = %d, want 5", meta.TotalLines)
	}

	// index.jsonl exists
	indexData, err := os.ReadFile(filepath.Join(dst, "index.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(indexData) == 0 {
		t.Error("index.jsonl is empty")
	}

	// data files exist
	var dataFiles []string
	dirEntries, _ := os.ReadDir(dst)
	for _, e := range dirEntries {
		if e.Name() != "metadata.json" && e.Name() != "index.jsonl" {
			dataFiles = append(dataFiles, e.Name())
		}
	}
	if len(dataFiles) == 0 {
		t.Error("no data files in output")
	}

	// can be opened with NewReader
	reader, err := NewReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	_, err = reader.Scan(nil, func(e recv.LogEntry) bool {
		count++
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("reader scan got %d entries, want 5", count)
	}
}

func TestSliceProgressCallback(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// need >10k entries to trigger mid-scan progress
	entries := makeEntries(100, base, "api")
	writeMetadata(t, dir, base, base.Add(100*time.Second), 100)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(99 * time.Second),
		Lines: 100,
		Bytes: 5000,
	}})

	dst := t.TempDir()
	var calls []SliceProgress
	err := Slice(dir, dst, nil, SliceConfig{Compress: false}, func(p SliceProgress) {
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
	if last.Matched != 100 {
		t.Errorf("final matched = %d, want 100", last.Matched)
	}
	if last.Total != 100 {
		t.Errorf("final total = %d, want 100", last.Total)
	}
}

func TestSliceMetadataInherited(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := makeEntries(5, base, "api")
	srcMeta := recv.Metadata{
		Version:    1,
		Format:     "jsonl+zstd",
		Started:    base,
		Stopped:    base.Add(5 * time.Second),
		TotalLines: 5,
		Redaction:  &recv.RedactionInfo{Enabled: true, Patterns: []string{"ssn"}},
	}
	data, err := json.MarshalIndent(srcMeta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(4 * time.Second),
		Lines: 5,
		Bytes: 300,
	}})

	dst := t.TempDir()
	err = Slice(dir, dst, nil, SliceConfig{Compress: false}, nil)
	if err != nil {
		t.Fatal(err)
	}

	outMeta, err := recv.ReadMetadata(dst)
	if err != nil {
		t.Fatal(err)
	}
	if outMeta.Version != 1 {
		t.Errorf("Version = %d, want 1", outMeta.Version)
	}
	if outMeta.Format != "jsonl+zstd" {
		t.Errorf("Format = %q, want %q", outMeta.Format, "jsonl+zstd")
	}
	if outMeta.Redaction == nil || !outMeta.Redaction.Enabled {
		t.Error("Redaction not inherited")
	}
	if !outMeta.Started.Equal(base) {
		t.Errorf("Started = %v, want %v", outMeta.Started, base)
	}
}

func TestSliceNoFilter(t *testing.T) {
	src, _ := setupSliceSource(t)
	dst := t.TempDir()

	err := Slice(src, dst, nil, SliceConfig{Compress: false}, nil)
	if err != nil {
		t.Fatal(err)
	}

	entries := readSlicedEntries(t, dst)
	if len(entries) != 10 {
		t.Fatalf("got %d entries, want 10", len(entries))
	}
}
