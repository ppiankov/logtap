package recv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTailerEntry(t *testing.T, f *os.File, entry LogEntry) {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func createTailerDir(t *testing.T) (string, *os.File) {
	t.Helper()
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "2024-01-15T100000-000.jsonl"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Write metadata.json with zero stopped time (live capture)
	meta := map[string]interface{}{
		"started": time.Now().Format(time.RFC3339),
		"stopped": time.Time{},
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), metaData, 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	return dir, f
}

func TestTailerReadLast(t *testing.T) {
	dir, f := createTailerDir(t)
	for i := 0; i < 20; i++ {
		writeTailerEntry(t, f, LogEntry{
			Timestamp: time.Date(2024, 1, 15, 10, 0, i, 0, time.UTC),
			Labels:    map[string]string{"app": "api"},
			Message:   "line",
		})
	}
	_ = f.Close()

	tailer, err := NewTailerFromStart(dir)
	if err != nil {
		t.Fatalf("new tailer: %v", err)
	}
	defer func() { _ = tailer.Close() }()

	last, err := tailer.ReadLast(5)
	if err != nil {
		t.Fatalf("read last: %v", err)
	}
	if len(last) != 5 {
		t.Errorf("len = %d, want 5", len(last))
	}
	// Should be the last 5 entries
	if last[0].Timestamp.Second() != 15 {
		t.Errorf("first entry second = %d, want 15", last[0].Timestamp.Second())
	}
}

func TestTailerReadLastMoreThanExists(t *testing.T) {
	dir, f := createTailerDir(t)
	for i := 0; i < 3; i++ {
		writeTailerEntry(t, f, LogEntry{
			Timestamp: time.Date(2024, 1, 15, 10, 0, i, 0, time.UTC),
			Message:   "line",
		})
	}
	_ = f.Close()

	tailer, err := NewTailerFromStart(dir)
	if err != nil {
		t.Fatalf("new tailer: %v", err)
	}
	defer func() { _ = tailer.Close() }()

	last, err := tailer.ReadLast(10)
	if err != nil {
		t.Fatalf("read last: %v", err)
	}
	if len(last) != 3 {
		t.Errorf("len = %d, want 3", len(last))
	}
}

func TestTailerTailNewLines(t *testing.T) {
	dir, f := createTailerDir(t)
	writeTailerEntry(t, f, LogEntry{Message: "initial"})
	_ = f.Sync()

	tailer, err := NewTailer(dir)
	if err != nil {
		t.Fatalf("new tailer: %v", err)
	}
	defer func() { _ = tailer.Close() }()

	// Nothing new yet (seeked to end)
	entries, err := tailer.Tail()
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("initial tail = %d entries, want 0", len(entries))
	}

	// Append new line
	writeTailerEntry(t, f, LogEntry{Message: "new line"})
	_ = f.Sync()

	entries, err = tailer.Tail()
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("tail = %d entries, want 1", len(entries))
	}
	if entries[0].Message != "new line" {
		t.Errorf("message = %q, want %q", entries[0].Message, "new line")
	}
	_ = f.Close()
}

func TestTailerRotation(t *testing.T) {
	dir, f := createTailerDir(t)
	writeTailerEntry(t, f, LogEntry{Message: "old"})
	_ = f.Close()

	tailer, err := NewTailer(dir)
	if err != nil {
		t.Fatalf("new tailer: %v", err)
	}
	defer func() { _ = tailer.Close() }()

	// Create a newer file
	time.Sleep(10 * time.Millisecond)
	f2, err := os.Create(filepath.Join(dir, "2024-01-15T100100-000.jsonl"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	writeTailerEntry(t, f2, LogEntry{Message: "rotated"})
	_ = f2.Sync()
	_ = f2.Close()

	entries, err := tailer.Tail()
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("tail = %d entries, want 1", len(entries))
	}
	if entries[0].Message != "rotated" {
		t.Errorf("message = %q, want %q", entries[0].Message, "rotated")
	}
}

func TestTailerNoJSONLFiles(t *testing.T) {
	dir := t.TempDir()
	_, err := NewTailer(dir)
	if err == nil {
		t.Error("expected error for empty dir")
	}
}

func TestIsLiveCapture(t *testing.T) {
	dir := t.TempDir()

	// No metadata = not live
	if IsLiveCapture(dir) {
		t.Error("expected not live without metadata")
	}

	// Zero stopped = live
	meta := `{"started":"2024-01-15T10:00:00Z","stopped":"0001-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(meta), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !IsLiveCapture(dir) {
		t.Error("expected live with zero stopped")
	}

	// Non-zero stopped = not live
	meta = `{"started":"2024-01-15T10:00:00Z","stopped":"2024-01-15T10:30:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(meta), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if IsLiveCapture(dir) {
		t.Error("expected not live with non-zero stopped")
	}
}

func TestNewestJSONL_SkipsSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	// These should be skipped
	_ = os.WriteFile(filepath.Join(dir, "index.jsonl"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "audit.jsonl"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "data.jsonl.zst"), []byte("compressed"), 0644)

	_, err := newestJSONL(dir)
	if err == nil {
		t.Error("expected error when only special files exist")
	}

	// Add a real data file
	_ = os.WriteFile(filepath.Join(dir, "2024-01-15T100000-000.jsonl"), []byte(`{"msg":"hi"}`+"\n"), 0644)
	name, err := newestJSONL(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "2024-01-15T100000-000.jsonl" {
		t.Errorf("name = %q, want data file", name)
	}
}
