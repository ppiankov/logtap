package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

func makeCaptureSub(t *testing.T, root, name string, started time.Time) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	meta := &recv.Metadata{
		Version: 1,
		Format:  "jsonl",
		Started: started,
	}
	if err := recv.WriteMetadata(dir, meta); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	// write a small data file to give the capture some size
	if err := os.WriteFile(filepath.Join(dir, "data.jsonl"), []byte(`{"msg":"test"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write data: %v", err)
	}
	return dir
}

func TestRunGC_MaxAge(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	// old capture — should be deleted
	makeCaptureSub(t, root, "old-capture", now.Add(-48*time.Hour))
	// recent capture — should survive
	recent := makeCaptureSub(t, root, "recent-capture", now.Add(-1*time.Hour))

	restore := redirectOutput(t)
	defer restore()

	err := runGC(root, "24h", "", false, false)
	if err != nil {
		t.Fatalf("runGC: %v", err)
	}

	// old should be gone
	if _, err := os.Stat(filepath.Join(root, "old-capture")); !os.IsNotExist(err) {
		t.Error("expected old-capture to be deleted")
	}
	// recent should remain
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("expected recent-capture to remain: %v", err)
	}
}

func TestRunGC_MaxTotal(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	// create two captures with some data
	makeCaptureSub(t, root, "cap-a", now.Add(-2*time.Hour))
	makeCaptureSub(t, root, "cap-b", now.Add(-1*time.Hour))

	restore := redirectOutput(t)
	defer restore()

	// set max total very small so oldest is deleted
	err := runGC(root, "", "1", false, false)
	if err != nil {
		t.Fatalf("runGC: %v", err)
	}

	// at least one should be deleted
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) >= 2 {
		t.Errorf("expected at least one capture to be deleted, got %d dirs", len(entries))
	}
}

func TestRunGC_DryRun(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	old := makeCaptureSub(t, root, "old-capture", now.Add(-48*time.Hour))

	restore := redirectOutput(t)
	defer restore()

	err := runGC(root, "24h", "", true, false)
	if err != nil {
		t.Fatalf("runGC dry-run: %v", err)
	}

	// dry run should NOT delete
	if _, err := os.Stat(old); err != nil {
		t.Error("expected old-capture to survive dry run")
	}
}

func TestRunGC_JSON(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	makeCaptureSub(t, root, "old-capture", now.Add(-48*time.Hour))

	restore := redirectOutput(t)
	defer restore()

	err := runGC(root, "24h", "", true, true)
	if err != nil {
		t.Fatalf("runGC json: %v", err)
	}
}

func TestRunGC_MissingFlags(t *testing.T) {
	err := runGC("/tmp", "", "", false, false)
	if err == nil {
		t.Error("expected error when neither --max-age nor --max-total provided")
	}
}

func TestRunGC_InvalidAge(t *testing.T) {
	err := runGC("/tmp", "notaduration", "", false, false)
	if err == nil || !strings.Contains(err.Error(), "max-age") {
		t.Errorf("expected --max-age error, got: %v", err)
	}
}

func TestRunGC_InvalidTotal(t *testing.T) {
	err := runGC("/tmp", "", "notasize", false, false)
	if err == nil || !strings.Contains(err.Error(), "max-total") {
		t.Errorf("expected --max-total error, got: %v", err)
	}
}

func TestRunGC_InvalidDir(t *testing.T) {
	err := runGC("/nonexistent/dir", "24h", "", false, false)
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestRunGC_NoDeletions(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	makeCaptureSub(t, root, "recent", now.Add(-1*time.Hour))

	restore := redirectOutput(t)
	defer restore()

	err := runGC(root, "48h", "", false, false)
	if err != nil {
		t.Fatalf("runGC: %v", err)
	}

	// capture should remain
	if _, err := os.Stat(filepath.Join(root, "recent")); err != nil {
		t.Error("expected recent capture to remain")
	}
}

func TestRunGC_EmptyDir(t *testing.T) {
	root := t.TempDir()

	restore := redirectOutput(t)
	defer restore()

	err := runGC(root, "24h", "", false, false)
	if err != nil {
		t.Fatalf("runGC empty: %v", err)
	}
}

func TestRunGC_JSONOutput(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	makeCaptureSub(t, root, "old", now.Add(-48*time.Hour))

	// capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	restore := func() {
		os.Stdout = oldStdout
	}

	gcErr := runGC(root, "24h", "", true, true)
	_ = w.Close()
	restore()

	if gcErr != nil {
		t.Fatalf("runGC json: %v", gcErr)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	var deletions []json.RawMessage
	if err := json.Unmarshal([]byte(output), &deletions); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, output)
	}
	if len(deletions) != 1 {
		t.Errorf("expected 1 deletion, got %d", len(deletions))
	}
}

func TestParseGCAge_Days(t *testing.T) {
	d, err := parseGCAge("7d")
	if err != nil {
		t.Fatalf("parseGCAge 7d: %v", err)
	}
	if d != 7*24*time.Hour {
		t.Errorf("duration = %v, want %v", d, 7*24*time.Hour)
	}
}

func TestParseGCAge_Hours(t *testing.T) {
	d, err := parseGCAge("24h")
	if err != nil {
		t.Fatalf("parseGCAge 24h: %v", err)
	}
	if d != 24*time.Hour {
		t.Errorf("duration = %v, want %v", d, 24*time.Hour)
	}
}

func TestParseGCAge_FractionalDays(t *testing.T) {
	d, err := parseGCAge("0.5d")
	if err != nil {
		t.Fatalf("parseGCAge 0.5d: %v", err)
	}
	if d != 12*time.Hour {
		t.Errorf("duration = %v, want %v", d, 12*time.Hour)
	}
}

func TestParseGCAge_Invalid(t *testing.T) {
	tests := []string{"", "d", "abc", "not-a-time"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := parseGCAge(input)
			if err == nil {
				t.Errorf("expected error for %q", input)
			}
		})
	}
}
