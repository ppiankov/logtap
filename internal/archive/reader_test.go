package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func writeMetadata(t *testing.T, dir string, started, stopped time.Time, lines int64) {
	t.Helper()
	meta := recv.Metadata{
		Version:    1,
		Format:     "jsonl",
		Started:    started,
		Stopped:    stopped,
		TotalLines: lines,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeIndex(t *testing.T, dir string, entries []rotate.IndexEntry) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, "index.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
			t.Fatal(err)
		}
	}
}

func writeDataFile(t *testing.T, dir, name string, entries []recv.LogEntry) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
			t.Fatal(err)
		}
	}
}

func writeCompressedDataFile(t *testing.T, dir, name string, entries []recv.LogEntry) {
	t.Helper()
	var raw []byte
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, data...)
		raw = append(raw, '\n')
	}

	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	compressed := enc.EncodeAll(raw, nil)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), compressed, 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeEntries(n int, base time.Time, app string) []recv.LogEntry {
	entries := make([]recv.LogEntry, n)
	for i := range entries {
		entries[i] = recv.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Labels:    map[string]string{"app": app},
			Message:   fmt.Sprintf("line %d", i),
		}
	}
	return entries
}

func TestReaderBasic(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	entries := makeEntries(10, base, "api")

	writeMetadata(t, dir, base, base.Add(10*time.Second), 10)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(9 * time.Second),
		Lines: 10,
		Bytes: 500,
	}})

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalLines() != 10 {
		t.Errorf("TotalLines = %d, want 10", r.TotalLines())
	}
	if r.Metadata().Version != 1 {
		t.Errorf("Version = %d, want 1", r.Metadata().Version)
	}

	var got []recv.LogEntry
	n, err := r.Scan(nil, func(e recv.LogEntry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Errorf("scanned = %d, want 10", n)
	}
	if len(got) != 10 {
		t.Errorf("got %d entries, want 10", len(got))
	}
	if got[0].Message != "line 0" {
		t.Errorf("first message = %q, want %q", got[0].Message, "line 0")
	}
	if got[9].Message != "line 9" {
		t.Errorf("last message = %q, want %q", got[9].Message, "line 9")
	}
}

func TestReaderZstd(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	entries := makeEntries(5, base, "web")

	writeMetadata(t, dir, base, base.Add(5*time.Second), 5)
	writeCompressedDataFile(t, dir, "2024-01-15T100000-000.jsonl.zst", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl.zst",
		From:  base,
		To:    base.Add(4 * time.Second),
		Lines: 5,
		Bytes: 300,
	}})

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}

	var got []recv.LogEntry
	_, err = r.Scan(nil, func(e recv.LogEntry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("got %d entries, want 5", len(got))
	}
	if got[0].Labels["app"] != "web" {
		t.Errorf("label app = %q, want %q", got[0].Labels["app"], "web")
	}
}

func TestReaderOrphanDiscovery(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	indexed := makeEntries(5, base, "api")
	orphan := makeEntries(3, base.Add(10*time.Second), "api")

	writeMetadata(t, dir, base, base.Add(15*time.Second), 8)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", indexed)
	writeDataFile(t, dir, "2024-01-15T100010-000.jsonl", orphan) // not in index
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(4 * time.Second),
		Lines: 5,
		Bytes: 300,
	}})

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}

	// TotalLines only counts indexed
	if r.TotalLines() != 5 {
		t.Errorf("TotalLines = %d, want 5", r.TotalLines())
	}

	var got []recv.LogEntry
	_, err = r.Scan(nil, func(e recv.LogEntry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	// should get all 8 entries: 5 indexed + 3 orphan
	if len(got) != 8 {
		t.Errorf("got %d entries, want 8", len(got))
	}
}

func TestReaderMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	file1 := makeEntries(5, base, "api")
	file2 := makeEntries(5, base.Add(10*time.Second), "web")

	writeMetadata(t, dir, base, base.Add(15*time.Second), 10)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", file1)
	writeDataFile(t, dir, "2024-01-15T100010-000.jsonl", file2)
	writeIndex(t, dir, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(4 * time.Second), Lines: 5},
		{File: "2024-01-15T100010-000.jsonl", From: base.Add(10 * time.Second), To: base.Add(14 * time.Second), Lines: 5},
	})

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}

	var got []recv.LogEntry
	_, err = r.Scan(nil, func(e recv.LogEntry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Errorf("got %d entries, want 10", len(got))
	}
	// verify order: file1 entries first, then file2
	if got[0].Labels["app"] != "api" {
		t.Errorf("first entry app = %q, want %q", got[0].Labels["app"], "api")
	}
	if got[5].Labels["app"] != "web" {
		t.Errorf("sixth entry app = %q, want %q", got[5].Labels["app"], "web")
	}
}

func TestReaderMissingMetadata(t *testing.T) {
	dir := t.TempDir()
	_, err := NewReader(dir)
	if err == nil {
		t.Error("expected error for missing metadata")
	}
}

func TestReaderEmptyCapture(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	writeMetadata(t, dir, base, base, 0)

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalLines() != 0 {
		t.Errorf("TotalLines = %d, want 0", r.TotalLines())
	}

	var got int
	_, err = r.Scan(nil, func(e recv.LogEntry) bool {
		got++
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("got %d entries, want 0", got)
	}
}

func TestReaderScanEarlyStop(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	entries := makeEntries(20, base, "api")

	writeMetadata(t, dir, base, base.Add(20*time.Second), 20)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(19 * time.Second), Lines: 20,
	}})

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}

	var got int
	_, err = r.Scan(nil, func(e recv.LogEntry) bool {
		got++
		return got < 5 // stop after 5
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != 5 {
		t.Errorf("got %d entries, want 5", got)
	}
}

func TestReaderNoIndex(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	entries := makeEntries(3, base, "api")

	writeMetadata(t, dir, base, base.Add(3*time.Second), 3)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	// no index.jsonl — all files are orphans

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalLines() != 0 {
		t.Errorf("TotalLines = %d, want 0 (no index)", r.TotalLines())
	}

	var got []recv.LogEntry
	_, err = r.Scan(nil, func(e recv.LogEntry) bool {
		got = append(got, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("got %d entries, want 3", len(got))
	}
}
