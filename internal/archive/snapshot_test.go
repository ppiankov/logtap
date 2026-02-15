package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func TestPackUnpackRoundTrip(t *testing.T) {
	src := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := makeEntries(100, base, "api")
	writeMetadata(t, src, base, base.Add(99*time.Second), 100)
	writeDataFile(t, src, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, src, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(99 * time.Second), Lines: 100},
	})

	// Pack
	archivePath := filepath.Join(t.TempDir(), "capture.tar.zst")
	if err := Pack(src, archivePath); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	// Verify archive file exists and is non-empty
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("archive stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("archive file is empty")
	}

	// Unpack
	dst := filepath.Join(t.TempDir(), "extracted")
	if err := Unpack(archivePath, dst); err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	// Verify metadata.json
	metaData, err := os.ReadFile(filepath.Join(dst, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var meta recv.Metadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta.TotalLines != 100 {
		t.Errorf("metadata total lines = %d, want 100", meta.TotalLines)
	}

	// Verify data file
	dataPath := filepath.Join(dst, "2024-01-15T100000-000.jsonl")
	if _, err := os.Stat(dataPath); err != nil {
		t.Fatalf("data file missing: %v", err)
	}

	// Verify extracted capture can be opened by Reader
	r, err := NewReader(dst)
	if err != nil {
		t.Fatalf("NewReader on extracted: %v", err)
	}
	n, err := r.Scan(nil, func(e recv.LogEntry) bool { return true })
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if n != 100 {
		t.Errorf("scanned %d entries, want 100", n)
	}
}

func TestPackUnpackCompressed(t *testing.T) {
	src := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := makeEntries(50, base, "web")
	writeMetadata(t, src, base, base.Add(49*time.Second), 50)
	writeCompressedDataFile(t, src, "2024-01-15T100000-000.jsonl.zst", entries)
	writeIndex(t, src, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl.zst", From: base, To: base.Add(49 * time.Second), Lines: 50},
	})

	archivePath := filepath.Join(t.TempDir(), "capture.tar.zst")
	if err := Pack(src, archivePath); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "extracted")
	if err := Unpack(archivePath, dst); err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	// Verify compressed data file preserved
	r, err := NewReader(dst)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	n, err := r.Scan(nil, func(e recv.LogEntry) bool { return true })
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if n != 50 {
		t.Errorf("scanned %d entries, want 50", n)
	}
}

func TestPackNotCaptureDir(t *testing.T) {
	src := t.TempDir() // no metadata.json
	archivePath := filepath.Join(t.TempDir(), "out.tar.zst")
	err := Pack(src, archivePath)
	if err == nil {
		t.Fatal("expected error for non-capture directory")
	}
}

func TestUnpackMissingMetadata(t *testing.T) {
	// Create a tar.zst with no metadata.json
	now := time.Now()
	src := t.TempDir()
	writeMetadata(t, src, now, now, 0)

	// Pack it
	archivePath := filepath.Join(t.TempDir(), "capture.tar.zst")
	if err := Pack(src, archivePath); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	// Now create a minimal archive with just a random file
	src2 := t.TempDir()
	// Write a fake metadata.json to make Pack happy
	if err := os.WriteFile(filepath.Join(src2, "metadata.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	archivePath2 := filepath.Join(t.TempDir(), "bad.tar.zst")
	if err := Pack(src2, archivePath2); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	err := Unpack(archivePath2, dst)
	if err == nil {
		t.Fatal("expected error for invalid metadata")
	}
}

func TestUnpackMissingIndex(t *testing.T) {
	now := time.Now()
	src := t.TempDir()
	writeMetadata(t, src, now, now, 0)
	// No index.jsonl

	archivePath := filepath.Join(t.TempDir(), "capture.tar.zst")
	if err := Pack(src, archivePath); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	err := Unpack(archivePath, dst)
	if err == nil {
		t.Fatal("expected error for missing index.jsonl")
	}
}

func TestSnapshot_CorruptTar(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "extracted")

	// Case 1: completely random bytes, not valid zstd at all
	badPath1 := filepath.Join(t.TempDir(), "garbage.tar.zst")
	if err := os.WriteFile(badPath1, []byte("this is not a tar.zst archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Unpack(badPath1, dst)
	if err == nil {
		t.Fatal("expected error for garbage data, got nil")
	}

	// Case 2: valid zstd wrapping garbage (not a valid tar)
	badPath2 := filepath.Join(t.TempDir(), "bad-tar.tar.zst")
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	compressed := enc.EncodeAll([]byte("not a tar stream at all, just random data"), nil)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath2, compressed, 0o644); err != nil {
		t.Fatal(err)
	}
	dst2 := filepath.Join(t.TempDir(), "extracted2")
	err = Unpack(badPath2, dst2)
	if err == nil {
		t.Fatal("expected error for corrupt tar inside valid zstd, got nil")
	}

	// Case 3: non-existent file
	err = Unpack("/nonexistent/path/archive.tar.zst", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("expected error for non-existent archive, got nil")
	}
}

func TestPackInvalidOutput(t *testing.T) {
	now := time.Now()
	src := t.TempDir()
	writeMetadata(t, src, now, now, 0)

	err := Pack(src, "/nonexistent/dir/out.tar.zst")
	if err == nil {
		t.Fatal("expected error for invalid output path")
	}
}
