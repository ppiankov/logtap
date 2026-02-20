package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

func writeMeta(t *testing.T, dir string, meta *recv.Metadata) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := recv.WriteMetadata(dir, meta); err != nil {
		t.Fatal(err)
	}
}

func TestCatalog_FindCaptures(t *testing.T) {
	root := t.TempDir()

	writeMeta(t, filepath.Join(root, "capture-a"), &recv.Metadata{
		Started:    time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		Stopped:    time.Date(2026, 2, 20, 11, 0, 0, 0, time.UTC),
		TotalLines: 1000,
		LabelsSeen: []string{"app"},
	})
	writeMeta(t, filepath.Join(root, "capture-b"), &recv.Metadata{
		Started:    time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC),
		Stopped:    time.Date(2026, 2, 20, 15, 0, 0, 0, time.UTC),
		TotalLines: 2000,
	})

	entries, err := Catalog(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	// sorted newest first
	if entries[0].Entries != 2000 {
		t.Errorf("first entry entries = %d, want 2000 (newest first)", entries[0].Entries)
	}
}

func TestCatalog_ActiveCapture(t *testing.T) {
	root := t.TempDir()

	writeMeta(t, filepath.Join(root, "live"), &recv.Metadata{
		Started:    time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		TotalLines: 500,
	})

	entries, err := Catalog(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if !entries[0].Active {
		t.Error("expected active=true for capture with zero stopped time")
	}
}

func TestCatalog_EmptyDir(t *testing.T) {
	root := t.TempDir()

	entries, err := Catalog(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %d entries, want 0", len(entries))
	}
}

func TestCatalog_Recursive(t *testing.T) {
	root := t.TempDir()

	writeMeta(t, filepath.Join(root, "team-a", "capture-1"), &recv.Metadata{
		Started:    time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		Stopped:    time.Date(2026, 2, 20, 11, 0, 0, 0, time.UTC),
		TotalLines: 100,
	})
	writeMeta(t, filepath.Join(root, "team-b", "capture-2"), &recv.Metadata{
		Started:    time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC),
		Stopped:    time.Date(2026, 2, 20, 13, 0, 0, 0, time.UTC),
		TotalLines: 200,
	})

	entries, err := Catalog(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

func TestCatalog_JSON(t *testing.T) {
	root := t.TempDir()

	writeMeta(t, filepath.Join(root, "cap"), &recv.Metadata{
		Started:    time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		Stopped:    time.Date(2026, 2, 20, 11, 0, 0, 0, time.UTC),
		TotalLines: 500,
	})

	entries, err := Catalog(root, false)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := WriteCatalogJSON(&buf, entries); err != nil {
		t.Fatal(err)
	}

	var parsed []CatalogEntry
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("got %d entries, want 1", len(parsed))
	}
	if parsed[0].Entries != 500 {
		t.Errorf("entries = %d, want 500", parsed[0].Entries)
	}
}

func TestCatalog_TextOutput(t *testing.T) {
	root := t.TempDir()

	writeMeta(t, filepath.Join(root, "cap"), &recv.Metadata{
		Started:    time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		Stopped:    time.Date(2026, 2, 20, 11, 0, 0, 0, time.UTC),
		TotalLines: 500,
	})

	entries, err := Catalog(root, false)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	WriteCatalogText(&buf, entries)
	if buf.Len() == 0 {
		t.Error("expected non-empty text output")
	}
}

func TestCatalog_EmptyText(t *testing.T) {
	var buf bytes.Buffer
	WriteCatalogText(&buf, nil)
	if buf.String() != "No captures found.\n" {
		t.Errorf("got %q, want 'No captures found.'", buf.String())
	}
}
