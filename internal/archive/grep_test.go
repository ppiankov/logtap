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

func TestGrep_Context(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// 7 entries: indices 0-6. Match at index 3.
	// With context=2, expect indices 1,2 (before), 3 (match), 4,5 (after).
	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "line 0"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "api"}, Message: "line 1"},
		{Timestamp: base.Add(2 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 2"},
		{Timestamp: base.Add(3 * time.Second), Labels: map[string]string{"app": "api"}, Message: "error: boom"},
		{Timestamp: base.Add(4 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 4"},
		{Timestamp: base.Add(5 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 5"},
		{Timestamp: base.Add(6 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 6"},
	}

	writeMetadata(t, dir, base, base.Add(7*time.Second), 7)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(6 * time.Second), Lines: 7,
	}})

	filter := &Filter{Grep: regexp.MustCompile("error")}

	var got []GrepMatch
	counts, err := Grep(dir, filter, GrepConfig{Context: 2}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should emit 5 entries: 2 before + 1 match + 2 after
	if len(got) != 5 {
		var msgs []string
		for _, m := range got {
			msgs = append(msgs, fmt.Sprintf("%s (ctx=%q)", m.Entry.Message, m.Context))
		}
		t.Fatalf("got %d entries %v, want 5", len(got), msgs)
	}

	// Verify context labels
	if got[0].Context != "before" || got[0].Entry.Message != "line 1" {
		t.Errorf("entry 0: context=%q msg=%q, want before/line 1", got[0].Context, got[0].Entry.Message)
	}
	if got[1].Context != "before" || got[1].Entry.Message != "line 2" {
		t.Errorf("entry 1: context=%q msg=%q, want before/line 2", got[1].Context, got[1].Entry.Message)
	}
	if got[2].Context != "" || got[2].Entry.Message != "error: boom" {
		t.Errorf("entry 2: context=%q msg=%q, want match", got[2].Context, got[2].Entry.Message)
	}
	if got[3].Context != "after" || got[3].Entry.Message != "line 4" {
		t.Errorf("entry 3: context=%q msg=%q, want after/line 4", got[3].Context, got[3].Entry.Message)
	}
	if got[4].Context != "after" || got[4].Entry.Message != "line 5" {
		t.Errorf("entry 4: context=%q msg=%q, want after/line 5", got[4].Context, got[4].Entry.Message)
	}

	// All entries should be in the same group
	for i, m := range got {
		if m.Group != 1 {
			t.Errorf("entry %d: group=%d, want 1", i, m.Group)
		}
	}

	// Match count should still be 1 (context entries are not counted as matches)
	if len(counts) != 1 || counts[0].Count != 1 {
		t.Errorf("counts = %v, want [{file 1}]", counts)
	}
}

func TestGrep_ContextOverlap(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// 8 entries: indices 0-7. Matches at index 2 and 4.
	// With context=1:
	//   match@2 -> span [1,3]
	//   match@4 -> span [3,5]
	// Merged: single span [1,5]. No duplicate for index 3.
	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "line 0"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "api"}, Message: "line 1"},
		{Timestamp: base.Add(2 * time.Second), Labels: map[string]string{"app": "api"}, Message: "error: first"},
		{Timestamp: base.Add(3 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 3"},
		{Timestamp: base.Add(4 * time.Second), Labels: map[string]string{"app": "api"}, Message: "error: second"},
		{Timestamp: base.Add(5 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 5"},
		{Timestamp: base.Add(6 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 6"},
		{Timestamp: base.Add(7 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 7"},
	}

	writeMetadata(t, dir, base, base.Add(8*time.Second), 8)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(7 * time.Second), Lines: 8,
	}})

	filter := &Filter{Grep: regexp.MustCompile("error")}

	var got []GrepMatch
	counts, err := Grep(dir, filter, GrepConfig{Context: 1}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Merged span [1,5]: line 1 (before), error: first (match), line 3 (between),
	// error: second (match), line 5 (after) = 5 entries
	if len(got) != 5 {
		var msgs []string
		for _, m := range got {
			msgs = append(msgs, fmt.Sprintf("%s (ctx=%q)", m.Entry.Message, m.Context))
		}
		t.Fatalf("got %d entries %v, want 5", len(got), msgs)
	}

	// Verify no duplicates — check all messages are unique
	seen := make(map[string]bool)
	for _, m := range got {
		if seen[m.Entry.Message] {
			t.Errorf("duplicate entry: %q", m.Entry.Message)
		}
		seen[m.Entry.Message] = true
	}

	// All entries should share the same group (overlapping ranges merged)
	for i, m := range got {
		if m.Group != 1 {
			t.Errorf("entry %d: group=%d, want 1", i, m.Group)
		}
	}

	// Match count should be 2
	if len(counts) != 1 || counts[0].Count != 2 {
		t.Errorf("counts = %v, want [{file 2}]", counts)
	}
}

func TestGrep_ContextZero(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "line 0"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "api"}, Message: "error: match"},
		{Timestamp: base.Add(2 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 2"},
	}

	writeMetadata(t, dir, base, base.Add(3*time.Second), 3)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(2 * time.Second), Lines: 3,
	}})

	filter := &Filter{Grep: regexp.MustCompile("error")}

	var got []GrepMatch
	counts, err := Grep(dir, filter, GrepConfig{Context: 0}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Context=0 should behave exactly like the default: only the match
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	if got[0].Entry.Message != "error: match" {
		t.Errorf("match = %q, want %q", got[0].Entry.Message, "error: match")
	}
	if got[0].Context != "" {
		t.Errorf("context = %q, want empty (direct match)", got[0].Context)
	}
	if got[0].Group != 0 {
		t.Errorf("group = %d, want 0 (no context mode)", got[0].Group)
	}
	if len(counts) != 1 || counts[0].Count != 1 {
		t.Errorf("counts = %v, want [{file 1}]", counts)
	}
}

func TestGrep_ContextSeparateGroups(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// 10 entries, matches at index 1 and 8. Context=1.
	// match@1 -> span [0,2], match@8 -> span [7,9]
	// These don't overlap, so should be 2 separate groups.
	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "line 0"},
		{Timestamp: base.Add(time.Second), Labels: map[string]string{"app": "api"}, Message: "error: early"},
		{Timestamp: base.Add(2 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 2"},
		{Timestamp: base.Add(3 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 3"},
		{Timestamp: base.Add(4 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 4"},
		{Timestamp: base.Add(5 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 5"},
		{Timestamp: base.Add(6 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 6"},
		{Timestamp: base.Add(7 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 7"},
		{Timestamp: base.Add(8 * time.Second), Labels: map[string]string{"app": "api"}, Message: "error: late"},
		{Timestamp: base.Add(9 * time.Second), Labels: map[string]string{"app": "api"}, Message: "line 9"},
	}

	writeMetadata(t, dir, base, base.Add(10*time.Second), 10)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(9 * time.Second), Lines: 10,
	}})

	filter := &Filter{Grep: regexp.MustCompile("error")}

	var got []GrepMatch
	_, err := Grep(dir, filter, GrepConfig{Context: 1}, func(m GrepMatch) {
		got = append(got, m)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Group 1: [0,2] = 3 entries. Group 2: [7,9] = 3 entries. Total: 6.
	if len(got) != 6 {
		var msgs []string
		for _, m := range got {
			msgs = append(msgs, fmt.Sprintf("%s (ctx=%q grp=%d)", m.Entry.Message, m.Context, m.Group))
		}
		t.Fatalf("got %d entries %v, want 6", len(got), msgs)
	}

	// First 3 should be group 1, last 3 should be group 2
	for i := 0; i < 3; i++ {
		if got[i].Group != 1 {
			t.Errorf("entry %d: group=%d, want 1", i, got[i].Group)
		}
	}
	for i := 3; i < 6; i++ {
		if got[i].Group != 2 {
			t.Errorf("entry %d: group=%d, want 2", i, got[i].Group)
		}
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
