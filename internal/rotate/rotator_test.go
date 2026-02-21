package rotate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestWriteBelowMaxFile(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"ts":"2024-01-01T00:00:00Z","msg":"hello"}` + "\n")
	if _, err := r.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// verify data was written
	entries, _ := os.ReadDir(dir)
	var found bool
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") && e.Name() != "index.jsonl" {
			content, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != string(data) {
				t.Errorf("got %q, want %q", content, data)
			}
			found = true
		}
	}
	if !found {
		t.Error("no data file found")
	}
}

func TestRotationTriggered(t *testing.T) {
	dir := t.TempDir()
	maxFile := int64(100)
	r, err := New(Config{Dir: dir, MaxFile: maxFile, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"ts":"2024-01-01T00:00:00Z","msg":"aaaaaaaaaa"}` + "\n")
	// write enough to trigger at least one rotation
	for i := 0; i < 10; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	files := dataFiles(t, dir)
	if len(files) < 2 {
		t.Errorf("expected at least 2 data files, got %d", len(files))
	}

	// verify index.jsonl has entries
	indexEntries := readIndex(t, dir)
	if len(indexEntries) == 0 {
		t.Error("index.jsonl has no entries")
	}
}

func TestCompression(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 50, MaxDisk: 1 << 20, Compress: true})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"ts":"2024-01-01T00:00:00Z","msg":"test"}` + "\n")
	// write enough to trigger rotation
	for i := 0; i < 5; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// verify .zst files exist and are valid
	entries, _ := os.ReadDir(dir)
	var zstCount int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl.zst") {
			zstCount++
			compressed, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			dec, err := zstd.NewReader(nil)
			if err != nil {
				t.Fatal(err)
			}
			decompressed, err := dec.DecodeAll(compressed, nil)
			dec.Close()
			if err != nil {
				t.Fatalf("invalid zstd file %s: %v", e.Name(), err)
			}
			if len(decompressed) == 0 {
				t.Errorf("decompressed %s is empty", e.Name())
			}
		}
	}
	if zstCount == 0 {
		t.Error("no .zst files found")
	}

	// verify index entries reference .zst files
	for _, entry := range readIndex(t, dir) {
		if !strings.HasSuffix(entry.File, ".jsonl.zst") {
			t.Errorf("index entry %s should end with .jsonl.zst", entry.File)
		}
	}
}

func TestIndexEntryMetadata(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 50, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)

	line := []byte(`{"ts":"2024-01-01T10:00:00Z","msg":"a"}` + "\n")
	r.TrackLine(ts1, map[string]string{"app": "test", "env": "dev"})
	if _, err := r.Write(line); err != nil {
		t.Fatal(err)
	}

	line2 := []byte(`{"ts":"2024-01-01T11:00:00Z","msg":"b"}` + "\n")
	r.TrackLine(ts2, map[string]string{"app": "test", "env": "prod"})
	if _, err := r.Write(line2); err != nil {
		t.Fatal(err)
	}

	// force rotation so index gets written
	for i := 0; i < 5; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	entries := readIndex(t, dir)
	if len(entries) == 0 {
		t.Fatal("no index entries")
	}

	first := entries[0]
	if first.Lines == 0 {
		t.Error("lines should be > 0")
	}
	if first.Bytes == 0 {
		t.Error("bytes should be > 0")
	}
}

func TestDiskCap(t *testing.T) {
	dir := t.TempDir()
	maxFile := int64(200)
	// allow roughly 3 files worth of data
	maxDisk := 3 * maxFile

	r, err := New(Config{Dir: dir, MaxFile: maxFile, MaxDisk: maxDisk})
	if err != nil {
		t.Fatal(err)
	}

	var warningFired int
	r.SetOnDiskWarning(func(usage, cap int64) {
		t.Logf("Disk warning fired: usage %d, cap %d", usage, cap)
		warningFired++
	})

	line := []byte(`{"ts":"2024-01-01T00:00:00Z","msg":"padding data for disk cap testing"}` + "\n")
	// write enough for ~5 rotations
	for i := 0; i < 50; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
		// Check for warning fired multiple times while writing
		if i > 0 && r.DiskUsage() > int64(float64(maxDisk)*0.8) && warningFired == 0 {
			t.Errorf("expected disk warning to fire, but it didn't after %d writes", i)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// Verify the warning fired at least once
	if warningFired == 0 {
		t.Error("expected disk warning to fire at least once, but it didn't")
	} else if warningFired > 1 {
		t.Logf("warning fired %d times, expected 1 (first time it crosses 80%% threshold)", warningFired)
	}

	usage := totalDiskUsage(t, dir)
	if usage > maxDisk+maxFile {
		// allow some slack for the active file + index
		t.Errorf("disk usage %d exceeds max %d by too much", usage, maxDisk)
	}

	// verify index only references existing files
	for _, entry := range readIndex(t, dir) {
		path := filepath.Join(dir, entry.File)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("index references deleted file: %s", entry.File)
		}
	}
}

func TestBootstrap(t *testing.T) {
	dir := t.TempDir()

	// pre-populate with files
	for i := 0; i < 3; i++ {
		name := filepath.Join(dir, fmt.Sprintf("pre-%d.jsonl", i))
		if err := os.WriteFile(name, make([]byte, 500), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	if r.DiskUsage() < 1500 {
		t.Errorf("expected DiskUsage >= 1500, got %d", r.DiskUsage())
	}
}

func TestCloseWritesIndex(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"final"}` + "\n")
	r.TrackLine(time.Now(), map[string]string{"app": "x"})
	if _, err := r.Write(line); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	entries := readIndex(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 index entry, got %d", len(entries))
	}
	if entries[0].Lines != 1 {
		t.Errorf("expected 1 line, got %d", entries[0].Lines)
	}
}

func TestFilePermissions(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 50, MaxDisk: 1 << 20, Compress: true})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"ts":"2024-01-01T00:00:00Z","msg":"perm test"}` + "\n")
	for i := 0; i < 5; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		perm := info.Mode().Perm()
		name := e.Name()

		switch {
		case strings.HasSuffix(name, ".jsonl.zst"):
			if perm != 0o640 {
				t.Errorf("%s permissions = %o, want 0640", name, perm)
			}
		case name == "index.jsonl":
			if perm != 0o640 {
				t.Errorf("%s permissions = %o, want 0640", name, perm)
			}
		}
	}
}

// helpers

func dataFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		name := e.Name()
		if name == "index.jsonl" || name == "metadata.json" {
			continue
		}
		if strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".jsonl.zst") {
			out = append(out, name)
		}
	}
	return out
}

func readIndex(t *testing.T, dir string) []IndexEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "index.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var entries []IndexEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e IndexEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("bad index line: %s: %v", line, err)
		}
		entries = append(entries, e)
	}
	return entries
}

func totalDiskUsage(t *testing.T, dir string) int64 {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}
