package archive

import (
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func TestMergeBasic(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// source 1: early entries
	src1 := t.TempDir()
	entries1 := makeEntries(5, base, "api")
	writeMetadata(t, src1, base, base.Add(5*time.Second), 5)
	writeDataFile(t, src1, "2024-01-15T100000-000.jsonl", entries1)
	writeIndex(t, src1, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(4 * time.Second), Lines: 5,
		Labels: map[string]map[string]int64{"app": {"api": 5}},
	}})

	// source 2: later entries
	src2 := t.TempDir()
	entries2 := makeEntries(3, base.Add(10*time.Second), "web")
	writeMetadata(t, src2, base.Add(10*time.Second), base.Add(13*time.Second), 3)
	writeDataFile(t, src2, "2024-01-15T100010-000.jsonl", entries2)
	writeIndex(t, src2, []rotate.IndexEntry{{
		File: "2024-01-15T100010-000.jsonl", From: base.Add(10 * time.Second), To: base.Add(12 * time.Second), Lines: 3,
		Labels: map[string]map[string]int64{"app": {"web": 3}},
	}})

	dst := t.TempDir()
	err := Merge([]string{src1, src2}, dst, nil)
	if err != nil {
		t.Fatal(err)
	}

	// verify merged capture is readable
	reader, err := NewReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	if reader.TotalLines() != 8 {
		t.Errorf("TotalLines = %d, want 8", reader.TotalLines())
	}

	var got []recv.LogEntry
	_, err = reader.Scan(nil, func(e recv.LogEntry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("got %d entries, want 8", len(got))
	}

	// verify metadata
	meta := reader.Metadata()
	if meta.Started != base {
		t.Errorf("Started = %v, want %v", meta.Started, base)
	}
}

func TestMergeNameCollision(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// both sources have same filename
	src1 := t.TempDir()
	entries1 := makeEntries(3, base, "api")
	writeMetadata(t, src1, base, base.Add(3*time.Second), 3)
	writeDataFile(t, src1, "2024-01-15T100000-000.jsonl", entries1)
	writeIndex(t, src1, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(2 * time.Second), Lines: 3,
	}})

	src2 := t.TempDir()
	entries2 := makeEntries(2, base.Add(time.Minute), "web")
	writeMetadata(t, src2, base.Add(time.Minute), base.Add(time.Minute+2*time.Second), 2)
	writeDataFile(t, src2, "2024-01-15T100000-000.jsonl", entries2) // same name!
	writeIndex(t, src2, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base.Add(time.Minute), To: base.Add(time.Minute + time.Second), Lines: 2,
	}})

	dst := t.TempDir()
	err := Merge([]string{src1, src2}, dst, nil)
	if err != nil {
		t.Fatal(err)
	}

	reader, err := NewReader(dst)
	if err != nil {
		t.Fatal(err)
	}

	// should have both files (one renamed)
	files := reader.Files()
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
	if files[0].Name == files[1].Name {
		t.Error("files should have different names after collision resolution")
	}

	// all entries should be readable
	var got []recv.LogEntry
	_, err = reader.Scan(nil, func(e recv.LogEntry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("got %d entries, want 5", len(got))
	}
}

func TestMergeCompressedFiles(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	src1 := t.TempDir()
	entries1 := makeEntries(3, base, "api")
	writeMetadata(t, src1, base, base.Add(3*time.Second), 3)
	writeCompressedDataFile(t, src1, "2024-01-15T100000-000.jsonl.zst", entries1)
	writeIndex(t, src1, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl.zst", From: base, To: base.Add(2 * time.Second), Lines: 3,
	}})

	src2 := t.TempDir()
	entries2 := makeEntries(2, base.Add(10*time.Second), "web")
	writeMetadata(t, src2, base.Add(10*time.Second), base.Add(12*time.Second), 2)
	writeCompressedDataFile(t, src2, "2024-01-15T100010-000.jsonl.zst", entries2)
	writeIndex(t, src2, []rotate.IndexEntry{{
		File: "2024-01-15T100010-000.jsonl.zst", From: base.Add(10 * time.Second), To: base.Add(11 * time.Second), Lines: 2,
	}})

	dst := t.TempDir()
	err := Merge([]string{src1, src2}, dst, nil)
	if err != nil {
		t.Fatal(err)
	}

	reader, err := NewReader(dst)
	if err != nil {
		t.Fatal(err)
	}

	var got []recv.LogEntry
	_, err = reader.Scan(nil, func(e recv.LogEntry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("got %d entries, want 5", len(got))
	}
}

func TestMergeOverlappingTimeRanges(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// overlapping: src1 10:00-10:05, src2 10:03-10:08
	src1 := t.TempDir()
	entries1 := makeEntries(5, base, "api")
	writeMetadata(t, src1, base, base.Add(5*time.Second), 5)
	writeDataFile(t, src1, "2024-01-15T100000-000.jsonl", entries1)
	writeIndex(t, src1, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(4 * time.Second), Lines: 5,
	}})

	src2 := t.TempDir()
	entries2 := makeEntries(5, base.Add(3*time.Second), "web")
	writeMetadata(t, src2, base.Add(3*time.Second), base.Add(8*time.Second), 5)
	writeDataFile(t, src2, "2024-01-15T100003-000.jsonl", entries2)
	writeIndex(t, src2, []rotate.IndexEntry{{
		File: "2024-01-15T100003-000.jsonl", From: base.Add(3 * time.Second), To: base.Add(7 * time.Second), Lines: 5,
	}})

	dst := t.TempDir()
	err := Merge([]string{src1, src2}, dst, nil)
	if err != nil {
		t.Fatal(err)
	}

	reader, err := NewReader(dst)
	if err != nil {
		t.Fatal(err)
	}

	var got []recv.LogEntry
	_, err = reader.Scan(nil, func(e recv.LogEntry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Errorf("got %d entries, want 10", len(got))
	}

	// index should be sorted by From time
	files := reader.Files()
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
	if files[0].Index != nil && files[1].Index != nil {
		if files[0].Index.From.After(files[1].Index.From) {
			t.Error("index not sorted by From time")
		}
	}
}

func TestMergeLabelsMerge(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	src1 := t.TempDir()
	writeMetadata(t, src1, base, base.Add(time.Second), 1)
	writeDataFile(t, src1, "2024-01-15T100000-000.jsonl", []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api", "env": "prod"}, Message: "m1"},
	})
	writeIndex(t, src1, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base, Lines: 1,
		Labels: map[string]map[string]int64{"app": {"api": 1}, "env": {"prod": 1}},
	}})

	src2 := t.TempDir()
	writeMetadata(t, src2, base.Add(time.Minute), base.Add(time.Minute+time.Second), 1)
	writeDataFile(t, src2, "2024-01-15T100100-000.jsonl", []recv.LogEntry{
		{Timestamp: base.Add(time.Minute), Labels: map[string]string{"app": "web", "region": "us"}, Message: "m2"},
	})
	writeIndex(t, src2, []rotate.IndexEntry{{
		File: "2024-01-15T100100-000.jsonl", From: base.Add(time.Minute), To: base.Add(time.Minute), Lines: 1,
		Labels: map[string]map[string]int64{"app": {"web": 1}, "region": {"us": 1}},
	}})

	dst := t.TempDir()
	err := Merge([]string{src1, src2}, dst, nil)
	if err != nil {
		t.Fatal(err)
	}

	meta, err := recv.ReadMetadata(dst)
	if err != nil {
		t.Fatal(err)
	}

	// should have union of all label keys
	labelSet := make(map[string]bool)
	for _, l := range meta.LabelsSeen {
		labelSet[l] = true
	}
	for _, key := range []string{"app", "env", "region"} {
		if !labelSet[key] {
			t.Errorf("missing label %q in merged metadata", key)
		}
	}
}

func TestMergeTooFewSources(t *testing.T) {
	err := Merge([]string{"/tmp/one"}, "/tmp/out", nil)
	if err == nil {
		t.Error("expected error for single source")
	}
}

func TestMergeProgress(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	src1 := t.TempDir()
	writeMetadata(t, src1, base, base.Add(time.Second), 1)
	writeDataFile(t, src1, "2024-01-15T100000-000.jsonl", makeEntries(1, base, "api"))
	writeIndex(t, src1, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base, Lines: 1,
	}})

	src2 := t.TempDir()
	writeMetadata(t, src2, base.Add(time.Minute), base.Add(time.Minute+time.Second), 1)
	writeDataFile(t, src2, "2024-01-15T100100-000.jsonl", makeEntries(1, base.Add(time.Minute), "web"))
	writeIndex(t, src2, []rotate.IndexEntry{{
		File: "2024-01-15T100100-000.jsonl", From: base.Add(time.Minute), To: base.Add(time.Minute), Lines: 1,
	}})

	dst := t.TempDir()
	var lastProgress MergeProgress
	progressCalled := false
	err := Merge([]string{src1, src2}, dst, func(p MergeProgress) {
		lastProgress = p
		progressCalled = true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !progressCalled {
		t.Fatal("progress not called")
	}
	if lastProgress.FilesCopied != 2 {
		t.Errorf("FilesCopied = %d, want 2", lastProgress.FilesCopied)
	}
	if lastProgress.TotalFiles != 2 {
		t.Errorf("TotalFiles = %d, want 2", lastProgress.TotalFiles)
	}
}

func TestMergeThreeSources(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	dirs := make([]string, 3)
	for i := 0; i < 3; i++ {
		dirs[i] = t.TempDir()
		offset := time.Duration(i) * 10 * time.Second
		entries := makeEntries(2, base.Add(offset), "svc")
		writeMetadata(t, dirs[i], base.Add(offset), base.Add(offset+2*time.Second), 2)
		writeDataFile(t, dirs[i], "2024-01-15T100000-000.jsonl", entries)
		writeIndex(t, dirs[i], []rotate.IndexEntry{{
			File: "2024-01-15T100000-000.jsonl", From: base.Add(offset), To: base.Add(offset + time.Second), Lines: 2,
		}})
	}

	dst := t.TempDir()
	err := Merge(dirs, dst, nil)
	if err != nil {
		t.Fatal(err)
	}

	reader, err := NewReader(dst)
	if err != nil {
		t.Fatal(err)
	}

	var got []recv.LogEntry
	_, err = reader.Scan(nil, func(e recv.LogEntry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 {
		t.Errorf("got %d entries, want 6", len(got))
	}
}
