package archive

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func TestGrepBasic(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "request started"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "api"}, Message: "error: connection refused"},
		{Timestamp: base.Add(2 * time.Second), Labels: map[string]string{"app": "api"}, Message: "request completed"},
		{Timestamp: base.Add(3 * time.Second), Labels: map[string]string{"app": "api"}, Message: "error: timeout exceeded"},
	}

	writeMetadata(t, dir, base, base.Add(4*time.Second), 4)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(3 * time.Second), Lines: 4,
		Labels: map[string]map[string]int64{"app": {"api": 4}},
	}})

	filter := &Filter{Grep: regexp.MustCompile("error")}

	var got []GrepMatch
	counts, err := Grep(dir, filter, GrepConfig{}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2", len(got))
	}
	if got[0].Entry.Message != "error: connection refused" {
		t.Errorf("first match = %q", got[0].Entry.Message)
	}
	if got[1].Entry.Message != "error: timeout exceeded" {
		t.Errorf("second match = %q", got[1].Entry.Message)
	}
	if got[0].File != "2024-01-15T100000-000.jsonl" {
		t.Errorf("file = %q", got[0].File)
	}
	if len(counts) != 1 || counts[0].Count != 2 {
		t.Errorf("counts = %v, want [{file 2}]", counts)
	}
}

func TestGrepNoMatches(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	entries := makeEntries(5, base, "api")

	writeMetadata(t, dir, base, base.Add(5*time.Second), 5)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(4 * time.Second), Lines: 5,
	}})

	filter := &Filter{Grep: regexp.MustCompile("NONEXISTENT_PATTERN")}

	var got []GrepMatch
	counts, err := Grep(dir, filter, GrepConfig{}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d matches, want 0", len(got))
	}
	if len(counts) != 0 {
		t.Errorf("counts = %v, want empty", counts)
	}
}

func TestGrepCountMode(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "error: one"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "api"}, Message: "ok"},
		{Timestamp: base.Add(2 * time.Second), Labels: map[string]string{"app": "api"}, Message: "error: two"},
	}

	writeMetadata(t, dir, base, base.Add(3*time.Second), 3)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(2 * time.Second), Lines: 3,
	}})

	filter := &Filter{Grep: regexp.MustCompile("error")}

	onMatchCalled := false
	counts, err := Grep(dir, filter, GrepConfig{CountOnly: true}, func(m GrepMatch) {
		onMatchCalled = true
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if onMatchCalled {
		t.Error("onMatch should not be called in count mode")
	}
	if len(counts) != 1 {
		t.Fatalf("counts len = %d, want 1", len(counts))
	}
	if counts[0].Count != 2 {
		t.Errorf("count = %d, want 2", counts[0].Count)
	}
}

func TestGrepWithTimeFilter(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	file1 := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "error: early"},
	}
	file2 := []recv.LogEntry{
		{Timestamp: base.Add(10 * time.Minute), Labels: map[string]string{"app": "api"}, Message: "error: late"},
	}

	writeMetadata(t, dir, base, base.Add(11*time.Minute), 2)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", file1)
	writeDataFile(t, dir, "2024-01-15T101000-000.jsonl", file2)
	writeIndex(t, dir, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl", From: base, To: base, Lines: 1,
			Labels: map[string]map[string]int64{"app": {"api": 1}}},
		{File: "2024-01-15T101000-000.jsonl", From: base.Add(10 * time.Minute), To: base.Add(10 * time.Minute), Lines: 1,
			Labels: map[string]map[string]int64{"app": {"api": 1}}},
	})

	// filter to only the second file's time range
	filter := &Filter{
		From: base.Add(5 * time.Minute),
		Grep: regexp.MustCompile("error"),
	}

	var got []GrepMatch
	counts, err := Grep(dir, filter, GrepConfig{}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	if got[0].Entry.Message != "error: late" {
		t.Errorf("match = %q, want %q", got[0].Entry.Message, "error: late")
	}
	if len(counts) != 1 || counts[0].File != "2024-01-15T101000-000.jsonl" {
		t.Errorf("unexpected counts: %v", counts)
	}
}

func TestGrepWithLabelFilter(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "error in api"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "web"}, Message: "error in web"},
	}

	writeMetadata(t, dir, base, base.Add(2*time.Second), 2)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(time.Second), Lines: 2,
		Labels: map[string]map[string]int64{"app": {"api": 1, "web": 1}},
	}})

	filter := &Filter{
		Labels: []LabelMatcher{{Key: "app", Value: "web"}},
		Grep:   regexp.MustCompile("error"),
	}

	var got []GrepMatch
	_, err := Grep(dir, filter, GrepConfig{}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	if got[0].Entry.Labels["app"] != "web" {
		t.Errorf("match label app = %q, want %q", got[0].Entry.Labels["app"], "web")
	}
}

func TestGrepCompressedFiles(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "error: compressed match"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "api"}, Message: "ok line"},
	}

	writeMetadata(t, dir, base, base.Add(2*time.Second), 2)
	writeCompressedDataFile(t, dir, "2024-01-15T100000-000.jsonl.zst", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl.zst", From: base, To: base.Add(time.Second), Lines: 2,
	}})

	filter := &Filter{Grep: regexp.MustCompile("compressed")}

	var got []GrepMatch
	_, err := Grep(dir, filter, GrepConfig{}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	if got[0].Entry.Message != "error: compressed match" {
		t.Errorf("match = %q", got[0].Entry.Message)
	}
}

func TestGrepMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	file1 := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "error: first file"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "api"}, Message: "ok"},
	}
	file2 := []recv.LogEntry{
		{Timestamp: base.Add(10 * time.Second), Labels: map[string]string{"app": "web"}, Message: "error: second file A"},
		{Timestamp: base.Add(11 * time.Second), Labels: map[string]string{"app": "web"}, Message: "error: second file B"},
		{Timestamp: base.Add(12 * time.Second), Labels: map[string]string{"app": "web"}, Message: "ok"},
	}

	writeMetadata(t, dir, base, base.Add(13*time.Second), 5)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", file1)
	writeDataFile(t, dir, "2024-01-15T100010-000.jsonl", file2)
	writeIndex(t, dir, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(time.Second), Lines: 2,
			Labels: map[string]map[string]int64{"app": {"api": 2}}},
		{File: "2024-01-15T100010-000.jsonl", From: base.Add(10 * time.Second), To: base.Add(12 * time.Second), Lines: 3,
			Labels: map[string]map[string]int64{"app": {"web": 3}}},
	})

	filter := &Filter{Grep: regexp.MustCompile("error")}

	var got []GrepMatch
	counts, err := Grep(dir, filter, GrepConfig{}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d matches, want 3", len(got))
	}
	if len(counts) != 2 {
		t.Fatalf("counts len = %d, want 2", len(counts))
	}
	if counts[0].Count != 1 {
		t.Errorf("file1 count = %d, want 1", counts[0].Count)
	}
	if counts[1].Count != 2 {
		t.Errorf("file2 count = %d, want 2", counts[1].Count)
	}
}

func TestGrepEmptyCapture(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	writeMetadata(t, dir, base, base, 0)

	filter := &Filter{Grep: regexp.MustCompile("anything")}

	var got []GrepMatch
	counts, err := Grep(dir, filter, GrepConfig{}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d matches, want 0", len(got))
	}
	if len(counts) != 0 {
		t.Errorf("counts = %v, want empty", counts)
	}
}

func TestGrepProgress(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	var entries []recv.LogEntry
	for i := 0; i < 10; i++ {
		msg := fmt.Sprintf("line %d", i)
		if i%3 == 0 {
			msg = fmt.Sprintf("error: line %d", i)
		}
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Labels:    map[string]string{"app": "api"},
			Message:   msg,
		})
	}

	writeMetadata(t, dir, base, base.Add(10*time.Second), 10)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(9 * time.Second), Lines: 10,
	}})

	filter := &Filter{Grep: regexp.MustCompile("error")}

	var lastProgress GrepProgress
	progressCalled := false
	_, err := Grep(dir, filter, GrepConfig{}, func(m GrepMatch) {}, func(p GrepProgress) {
		lastProgress = p
		progressCalled = true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !progressCalled {
		t.Fatal("progress callback not called")
	}
	if lastProgress.Scanned != 10 {
		t.Errorf("scanned = %d, want 10", lastProgress.Scanned)
	}
	if lastProgress.Total != 10 {
		t.Errorf("total = %d, want 10", lastProgress.Total)
	}
	// entries 0, 3, 6, 9 match "error"
	if lastProgress.Matches != 4 {
		t.Errorf("matches = %d, want 4", lastProgress.Matches)
	}
}

func TestGrep_InvalidRegex(t *testing.T) {
	// Verify that an invalid regex pattern fails at compilation,
	// preventing it from reaching Grep via Filter.Grep.
	patterns := []string{
		"[invalid",
		"(?P<>bad)",
		"*quantifier",
		"(unclosed",
	}

	for _, p := range patterns {
		_, err := regexp.Compile(p)
		if err == nil {
			t.Errorf("expected compile error for pattern %q", p)
			continue
		}
		if !strings.Contains(err.Error(), "error parsing regexp") {
			t.Errorf("unexpected error type for %q: %v", p, err)
		}
	}

	// Also verify Grep returns an error for a non-existent source directory
	// (the primary error path within Grep itself)
	filter := &Filter{Grep: regexp.MustCompile("anything")}
	_, err := Grep("/nonexistent/capture/dir", filter, GrepConfig{}, func(m GrepMatch) {}, nil)
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}
