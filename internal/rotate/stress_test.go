package rotate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRotationUnderConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	maxFile := int64(200)
	maxDisk := int64(10 * 1024) // 10 KB

	r, err := New(Config{Dir: dir, MaxFile: maxFile, MaxDisk: maxDisk})
	if err != nil {
		t.Fatal(err)
	}

	const numWriters = 10
	const linesPerWriter = 100
	line := []byte(`{"ts":"2024-01-01T00:00:00Z","msg":"concurrent write test padding"}` + "\n")

	var wg sync.WaitGroup
	var writeErrors atomic.Int64

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < linesPerWriter; j++ {
				if _, err := r.Write(line); err != nil {
					writeErrors.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	if writeErrors.Load() != 0 {
		t.Errorf("got %d write errors", writeErrors.Load())
	}

	usage := stressTotalDiskUsage(t, dir)
	limit := maxDisk + maxFile // active file slack
	if usage > limit {
		t.Errorf("disk usage %d exceeds limit %d (maxDisk + maxFile)", usage, limit)
	}

	// all index entries should reference existing files
	for _, entry := range stressReadIndex(t, dir) {
		path := filepath.Join(dir, entry.File)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("index references deleted file: %s", entry.File)
		}
	}
}

func TestTightDiskCap(t *testing.T) {
	dir := t.TempDir()
	maxFile := int64(100)
	maxDisk := maxFile // only 1 file worth

	r, err := New(Config{Dir: dir, MaxFile: maxFile, MaxDisk: maxDisk})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"ts":"2024-01-01T00:00:00Z","msg":"tight cap"}` + "\n")
	// write enough for ~10 rotations
	for i := 0; i < 50; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// verify disk stays bounded
	usage := stressTotalDiskUsage(t, dir)
	// allow active file + maxDisk + index overhead
	limit := maxDisk + maxFile + 1024 // 1KB index slack
	if usage > limit {
		t.Errorf("disk usage %d exceeds tight limit %d", usage, limit)
	}

	// verify all index entries reference existing files
	for _, entry := range stressReadIndex(t, dir) {
		path := filepath.Join(dir, entry.File)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("index references deleted file: %s", entry.File)
		}
	}
}

func TestHighRotationFrequency(t *testing.T) {
	dir := t.TempDir()
	r, err := New(Config{Dir: dir, MaxFile: 50, MaxDisk: 5000})
	if err != nil {
		t.Fatal(err)
	}

	line := []byte(`{"ts":"2024-01-01T00:00:00Z","msg":"hi"}` + "\n")
	for i := 0; i < 200; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// should have many index entries
	entries := stressReadIndex(t, dir)
	if len(entries) < 10 {
		t.Errorf("expected >10 index entries with tiny MaxFile, got %d", len(entries))
	}

	// verify all referenced files exist and contain valid JSONL
	for _, entry := range entries {
		path := filepath.Join(dir, entry.File)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("index references deleted file: %s", entry.File)
			continue
		}

		// only validate uncompressed files
		if strings.HasSuffix(entry.File, ".jsonl") {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read %s: %v", entry.File, err)
				continue
			}
			for _, jsonlLine := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if jsonlLine == "" {
					continue
				}
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(jsonlLine), &obj); err != nil {
					t.Errorf("invalid JSON in %s: %v", entry.File, err)
				}
			}
		}
	}

	// disk usage within bounds
	usage := stressTotalDiskUsage(t, dir)
	if usage > 5000+50+1024 { // maxDisk + maxFile + index slack
		t.Errorf("disk usage %d exceeds limit", usage)
	}
}

// helpers with unique names to avoid conflict with rotator_test.go

func stressReadIndex(t *testing.T, dir string) []IndexEntry {
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

func stressTotalDiskUsage(t *testing.T, dir string) int64 {
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
