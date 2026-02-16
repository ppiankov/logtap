package rotate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCloseWithoutData(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	// close without writing any data — should write no index entry
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	entries := readIndex(t, dir)
	if len(entries) != 0 {
		t.Errorf("expected 0 index entries for empty file, got %d", len(entries))
	}
}

func TestCloseNilActive(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	// close once to set active to nil
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// close again — should be safe
	if err := r.Close(); err != nil {
		t.Errorf("double close should return nil, got %v", err)
	}
}

func TestCloseWithCompressionNoData(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20, Compress: true})
	if err != nil {
		t.Fatal(err)
	}

	// close without data — should not attempt compression
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseWithCompressionWithData(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20, Compress: true})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"compress me"}` + "\n")
	r.TrackLine(time.Now(), nil)
	if _, err := r.Write(line); err != nil {
		t.Fatal(err)
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// verify .zst file was created for the final close
	entries := readIndex(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 index entry, got %d", len(entries))
	}
	if filepath.Ext(entries[0].File) != ".zst" {
		t.Errorf("expected .zst extension, got %s", entries[0].File)
	}
}

func TestDiskCapNoEnforcement(t *testing.T) {
	dir := t.TempDir()
	// MaxDisk 0 means no cap
	r, err := New(Config{Dir: dir, MaxFile: 100, MaxDisk: 0})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"unlimited disk"}` + "\n")
	for i := 0; i < 20; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// all files should still exist — no eviction
	files := dataFiles(t, dir)
	if len(files) < 3 {
		t.Errorf("expected multiple data files, got %d", len(files))
	}
}

func TestDiskCapUnderLimit(t *testing.T) {
	dir := t.TempDir()
	// huge disk cap — nothing should be evicted
	r, err := New(Config{Dir: dir, MaxFile: 100, MaxDisk: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"under limit"}` + "\n")
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
		t.Errorf("expected multiple data files, got %d", len(files))
	}
}

func TestTrackLineMultipleLabels(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	ts1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	ts2 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	ts3 := time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC)

	r.TrackLine(ts1, map[string]string{"app": "svc-a"})
	r.TrackLine(ts2, map[string]string{"app": "svc-b"})
	r.TrackLine(ts3, map[string]string{"app": "svc-a", "env": "prod"})

	line := []byte(`{"msg":"track"}` + "\n")
	for i := 0; i < 3; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}

	// force rotation to verify index metadata
	for i := 0; i < 200; i++ {
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

	// first entry should have label data
	first := entries[0]
	if first.Labels == nil {
		t.Error("expected labels in index entry")
	}
	if first.Lines < 3 {
		t.Errorf("expected at least 3 lines, got %d", first.Lines)
	}
}

func TestPruneIndexMissingFile(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	// pruneIndex with no index file should be safe
	err = r.pruneIndex(map[string]bool{"fake.jsonl": true})
	if err != nil {
		t.Errorf("pruneIndex should handle missing index: %v", err)
	}
}

func TestPruneIndexWithEntries(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 50, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"prune test data here!!"}` + "\n")
	r.TrackLine(time.Now(), nil)
	for i := 0; i < 10; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	entries := readIndex(t, dir)
	if len(entries) < 2 {
		t.Fatalf("need at least 2 entries, got %d", len(entries))
	}

	// prune first entry
	deleted := map[string]bool{entries[0].File: true}
	r2, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r2.Close() }()

	if err := r2.pruneIndex(deleted); err != nil {
		t.Fatal(err)
	}

	remaining := readIndex(t, dir)
	if len(remaining) >= len(entries) {
		t.Errorf("expected fewer entries after prune: before=%d, after=%d", len(entries), len(remaining))
	}
}

func TestOnRotateCallback(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 30, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	var rotateCount int
	var lastReason string
	r.SetOnRotate(func(reason string) {
		rotateCount++
		lastReason = reason
	})

	var errorCount int
	r.SetOnError(func() {
		errorCount++
	})

	line := []byte(`{"msg":"callback test data"}` + "\n")
	for i := 0; i < 5; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	if rotateCount == 0 {
		t.Error("expected onRotate callback to fire at least once")
	}
	if lastReason != "size" {
		t.Errorf("expected reason %q, got %q", "size", lastReason)
	}
	if errorCount != 0 {
		t.Errorf("expected 0 error callbacks, got %d", errorCount)
	}
}

func TestNewWithBadDir(t *testing.T) {
	_, err := New(Config{Dir: "/proc/0/nonexistent", MaxFile: 4096, MaxDisk: 1 << 20})
	if err == nil {
		t.Error("expected error for bad directory")
	}
}

func TestBootstrapWithSubdirs(t *testing.T) {
	dir := t.TempDir()

	// create a subdirectory — bootstrap should skip it
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// create a regular file
	if err := os.WriteFile(filepath.Join(dir, "existing.jsonl"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	if r.DiskUsage() < 100 {
		t.Errorf("expected DiskUsage >= 100, got %d", r.DiskUsage())
	}
}

func TestNextFilenameSequence(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 30, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"seq test"}` + "\n")
	// write enough to trigger multiple rotations in same second
	for i := 0; i < 20; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	files := dataFiles(t, dir)
	if len(files) < 3 {
		t.Errorf("expected multiple files from same-second rotations, got %d", len(files))
	}
}

func TestCompressedRotation(t *testing.T) {
	dir := t.TempDir()
	// small MaxFile to trigger many rotations with compression
	r, err := New(Config{Dir: dir, MaxFile: 40, MaxDisk: 1 << 20, Compress: true})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"compress rotation test"}` + "\n")
	for i := 0; i < 20; i++ {
		r.TrackLine(time.Now(), map[string]string{"app": "comp"})
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// verify compressed files and index entries
	entries := readIndex(t, dir)
	if len(entries) < 2 {
		t.Fatalf("expected multiple index entries, got %d", len(entries))
	}

	for _, e := range entries {
		if filepath.Ext(e.File) != ".zst" {
			t.Errorf("expected .zst extension: %s", e.File)
		}
		// verify file exists
		if _, err := os.Stat(filepath.Join(dir, e.File)); err != nil {
			t.Errorf("compressed file missing: %s", e.File)
		}
	}
}

func TestCompressedDiskCap(t *testing.T) {
	dir := t.TempDir()
	// small max disk to trigger eviction with compression
	r, err := New(Config{Dir: dir, MaxFile: 40, MaxDisk: 300, Compress: true})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"disk cap with compression"}` + "\n")
	for i := 0; i < 30; i++ {
		r.TrackLine(time.Now(), nil)
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// verify disk usage is reasonable
	usage := totalDiskUsage(t, dir)
	if usage > 500 {
		t.Errorf("disk usage %d exceeds expected cap", usage)
	}

	// verify index only references existing files
	for _, entry := range readIndex(t, dir) {
		path := filepath.Join(dir, entry.File)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("index references deleted file: %s", entry.File)
		}
	}
}

func TestWriteTriggersRotation(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 30, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"trigger rotation"}` + "\n")
	// first write fills past MaxFile, second triggers rotate
	if _, err := r.Write(line); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write(line); err != nil {
		t.Fatal(err)
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	files := dataFiles(t, dir)
	if len(files) < 2 {
		t.Errorf("expected rotation to create at least 2 files, got %d", len(files))
	}
}

func TestWriteRotationFailure(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 30, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"fill to trigger rotate"}` + "\n")
	// first write fills the file
	if _, err := r.Write(line); err != nil {
		t.Fatal(err)
	}

	// make dir read-only so openNew inside rotate() fails
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	// second write triggers rotation, which should fail on openNew
	_, err = r.Write(line)
	if err == nil {
		t.Error("expected error when rotation fails due to read-only dir")
	}
}

func TestAppendIndexError(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"index error test"}` + "\n")
	r.TrackLine(time.Now(), nil)
	if _, err := r.Write(line); err != nil {
		t.Fatal(err)
	}

	// create index.jsonl as a directory to make appendIndex fail
	indexPath := filepath.Join(dir, "index.jsonl")
	_ = os.Remove(indexPath) // remove if exists
	if err := os.MkdirAll(indexPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Close writes index entry, which should fail
	err = r.Close()
	if err == nil {
		t.Error("expected error when appendIndex fails")
	}
}

func TestEnforceDiskCapReadDirError(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 30, MaxDisk: 100})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"msg":"cap error test data!!"}` + "\n")
	// write enough to trigger rotation
	if _, err := r.Write(line); err != nil {
		t.Fatal(err)
	}

	// close the active file manually so rotate doesn't fail on active.Close()
	// Instead, just make dir unreadable right after writing so enforceDiskCap fails
	// We need to be more surgical: create a symlink that breaks ReadDir
	// Actually, just rename the dir temporarily during enforceDiskCap
	// Simplest: close properly, then call enforceDiskCap on a broken dir
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// create a new rotator, make dir unreadable, call enforceDiskCap directly
	r2, err := New(Config{Dir: dir, MaxFile: 30, MaxDisk: 1})
	if err != nil {
		t.Fatal(err)
	}

	// remove dir to make ReadDir fail
	tmpDir := dir + ".bak"
	if err := os.Rename(dir, tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(tmpDir, dir) })

	err = r2.enforceDiskCap()
	if err == nil {
		t.Error("expected error when dir is missing for enforceDiskCap")
	}
}

func TestOnDiskWarningCallback(t *testing.T) {
	dir := t.TempDir()
	maxDisk := int64(300)
	r, err := New(Config{Dir: dir, MaxFile: 30, MaxDisk: maxDisk})
	if err != nil {
		t.Fatal(err)
	}

	var warningUsage, warningCap int64
	var warningCount int
	r.SetOnDiskWarning(func(usage, cap int64) {
		warningCount++
		warningUsage = usage
		warningCap = cap
	})

	line := []byte(`{"msg":"disk warning test data!!"}` + "\n")
	for i := 0; i < 30; i++ {
		r.TrackLine(time.Now(), nil)
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	if warningCount == 0 {
		t.Error("expected disk warning callback to fire")
	}
	if warningCap != maxDisk {
		t.Errorf("warning cap = %d, want %d", warningCap, maxDisk)
	}
	if warningUsage == 0 {
		t.Error("warning usage should be > 0")
	}
}

func TestDiskUsageGetter(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 4096, MaxDisk: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	initialUsage := r.DiskUsage()

	line := []byte(`{"msg":"usage tracking test line"}` + "\n")
	if _, err := r.Write(line); err != nil {
		t.Fatal(err)
	}

	afterUsage := r.DiskUsage()
	if afterUsage <= initialUsage {
		t.Errorf("disk usage should increase after write: before=%d, after=%d", initialUsage, afterUsage)
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}
