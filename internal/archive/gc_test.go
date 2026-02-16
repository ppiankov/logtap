package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGC_MaxAge(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)

	oldA := createCapture(t, root, "old-a", now.Add(-10*24*time.Hour), 16)
	oldB := createCapture(t, root, "old-b", now.Add(-8*24*time.Hour), 16)
	fresh := createCapture(t, root, "fresh", now.Add(-2*24*time.Hour), 16)

	result, err := GC(root, GCOptions{MaxAge: 7 * 24 * time.Hour, Now: now})
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}

	assertMissing(t, oldA)
	assertMissing(t, oldB)
	assertExists(t, fresh)

	if len(result.Deletions) != 2 {
		t.Fatalf("deletions = %d, want 2", len(result.Deletions))
	}
}

func TestGC_MaxTotal(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	oldDir := createCapture(t, root, "old", now.Add(-10*time.Hour), 64)
	midDir := createCapture(t, root, "mid", now.Add(-5*time.Hour), 32)
	newDir := createCapture(t, root, "new", now.Add(-1*time.Hour), 16)

	sizeOld, err := dirSize(oldDir)
	if err != nil {
		t.Fatalf("dirSize old: %v", err)
	}
	sizeMid, err := dirSize(midDir)
	if err != nil {
		t.Fatalf("dirSize mid: %v", err)
	}
	sizeNew, err := dirSize(newDir)
	if err != nil {
		t.Fatalf("dirSize new: %v", err)
	}

	total := sizeOld + sizeMid + sizeNew
	maxTotal := total - sizeOld + 1

	_, err = GC(root, GCOptions{MaxTotalBytes: maxTotal, Now: now})
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}

	assertMissing(t, oldDir)
	assertExists(t, midDir)
	assertExists(t, newDir)
}

func TestGC_DryRun(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	oldDir := createCapture(t, root, "old", now.Add(-48*time.Hour), 8)

	result, err := GC(root, GCOptions{MaxAge: 24 * time.Hour, DryRun: true, Now: now})
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}

	assertExists(t, oldDir)

	if len(result.Deletions) != 1 {
		t.Fatalf("deletions = %d, want 1", len(result.Deletions))
	}
}

func TestGC_SkipNonCapture(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)

	nonCapture := filepath.Join(root, "misc")
	if err := os.MkdirAll(nonCapture, 0o755); err != nil {
		t.Fatalf("mkdir misc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nonCapture, "note.txt"), []byte("noop"), 0o644); err != nil {
		t.Fatalf("write misc file: %v", err)
	}

	oldDir := createCapture(t, root, "old", now.Add(-10*24*time.Hour), 8)

	_, err := GC(root, GCOptions{MaxAge: 7 * 24 * time.Hour, Now: now})
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}

	assertExists(t, nonCapture)
	assertMissing(t, oldDir)
}

func TestGC_EmptyDir(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)

	result, err := GC(root, GCOptions{MaxAge: 24 * time.Hour, Now: now})
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}
	if len(result.Deletions) != 0 {
		t.Fatalf("deletions = %d, want 0", len(result.Deletions))
	}
}

func TestGC_BothFlags(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	oldDir := createCapture(t, root, "old", now.Add(-10*24*time.Hour), 48)
	midDir := createCapture(t, root, "mid", now.Add(-3*24*time.Hour), 32)
	newDir := createCapture(t, root, "new", now.Add(-12*time.Hour), 16)

	sizeNew, err := dirSize(newDir)
	if err != nil {
		t.Fatalf("dirSize new: %v", err)
	}

	_, err = GC(root, GCOptions{MaxAge: 7 * 24 * time.Hour, MaxTotalBytes: sizeNew, Now: now})
	if err != nil {
		t.Fatalf("GC error: %v", err)
	}

	assertMissing(t, oldDir)
	assertMissing(t, midDir)
	assertExists(t, newDir)
}

func TestGCResult_WriteJSON(t *testing.T) {
	result := &GCResult{
		Root:         "/captures",
		CaptureCount: 3,
		TotalBytes:   1024,
		DryRun:       false,
		Deletions: []GCDeletion{
			{Dir: "/captures/old", Started: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), SizeBytes: 512, Reasons: []string{"max-age"}},
		},
	}
	var buf bytes.Buffer
	if err := result.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "/captures/old") {
		t.Errorf("expected dir in JSON output, got: %s", out)
	}
	if !strings.Contains(out, "max-age") {
		t.Errorf("expected reason in JSON output, got: %s", out)
	}
	// Verify valid JSON
	var parsed []GCDeletion
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("WriteJSON produced invalid JSON: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 deletion, got %d", len(parsed))
	}
}

func TestGCResult_WriteText(t *testing.T) {
	now := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	result := &GCResult{
		Root:         "/captures",
		CaptureCount: 3,
		TotalBytes:   2048,
		DryRun:       true,
		Deletions: []GCDeletion{
			{Dir: "/captures/old", Started: now.Add(-48 * time.Hour), SizeBytes: 512, Reasons: []string{"max-age"}},
			{Dir: "/captures/big", Started: now.Add(-1 * time.Hour), SizeBytes: 1024, Reasons: []string{"max-total"}},
		},
		now: now,
	}
	var buf bytes.Buffer
	result.WriteText(&buf)
	out := buf.String()
	if !strings.Contains(out, "Captures: 3") {
		t.Errorf("expected capture count, got: %s", out)
	}
	if !strings.Contains(out, "Dry run") {
		t.Errorf("expected dry run notice, got: %s", out)
	}
	if !strings.Contains(out, "/captures/old") {
		t.Errorf("expected old dir in output, got: %s", out)
	}
	if !strings.Contains(out, "max-age") {
		t.Errorf("expected reason in output, got: %s", out)
	}
}

func TestGCResult_WriteText_NoDeletions(t *testing.T) {
	result := &GCResult{
		Root:         "/captures",
		CaptureCount: 2,
		TotalBytes:   512,
		DryRun:       false,
		Deletions:    nil,
	}
	var buf bytes.Buffer
	result.WriteText(&buf)
	out := buf.String()
	if !strings.Contains(out, "Deleted 0") {
		t.Errorf("expected 'Deleted 0', got: %s", out)
	}
}

func TestGCResult_WriteText_ZeroStarted(t *testing.T) {
	result := &GCResult{
		Root:         "/captures",
		CaptureCount: 1,
		TotalBytes:   256,
		DryRun:       false,
		Deletions: []GCDeletion{
			{Dir: "/captures/unknown", SizeBytes: 256, Reasons: []string{"max-age"}},
		},
	}
	var buf bytes.Buffer
	result.WriteText(&buf)
	out := buf.String()
	if !strings.Contains(out, "unknown") {
		t.Errorf("expected 'unknown' in output, got: %s", out)
	}
}

func createCapture(t *testing.T, root, name string, started time.Time, size int) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir capture: %v", err)
	}
	writeMetadata(t, dir, started, started.Add(time.Minute), 0)
	if size > 0 {
		data := bytes.Repeat([]byte{'a'}, size)
		if err := os.WriteFile(filepath.Join(dir, "data.jsonl"), data, 0o644); err != nil {
			t.Fatalf("write data file: %v", err)
		}
	}
	return dir
}

func assertExists(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected %s to exist: %v", dir, err)
	}
}

func assertMissing(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("expected %s to be deleted", dir)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", dir, err)
	}
}
