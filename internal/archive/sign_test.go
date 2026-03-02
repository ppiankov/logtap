package archive

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/rotate"
)

func TestSign_NormalCapture(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	writeMetadata(t, dir, base, base.Add(99*time.Second), 100)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", makeEntries(100, base, "api"))
	writeIndex(t, dir, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(99 * time.Second), Lines: 100},
	})

	result, err := Sign(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.RootHash == "" {
		t.Error("expected non-empty root hash")
	}
	if len(result.Files) != 3 {
		t.Errorf("expected 3 files, got %d", len(result.Files))
	}

	// manifest.sha256 should exist
	if _, err := os.Stat(filepath.Join(dir, manifestFile)); err != nil {
		t.Errorf("manifest file not created: %v", err)
	}
}

func TestSign_EmptyCapture(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	writeMetadata(t, dir, base, base, 0)

	result, err := Sign(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 {
		t.Errorf("expected 1 file (metadata.json), got %d", len(result.Files))
	}
}

func TestSign_Deterministic(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	writeMetadata(t, dir, base, base.Add(99*time.Second), 50)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", makeEntries(50, base, "web"))
	writeIndex(t, dir, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(49 * time.Second), Lines: 50},
	})

	r1, err := Sign(dir)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Sign(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r1.RootHash != r2.RootHash {
		t.Errorf("root hashes differ: %s vs %s", r1.RootHash, r2.RootHash)
	}
}

func TestVerify_ValidCapture(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	writeMetadata(t, dir, base, base.Add(99*time.Second), 100)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", makeEntries(100, base, "api"))
	writeIndex(t, dir, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(99 * time.Second), Lines: 100},
	})

	if _, err := Sign(dir); err != nil {
		t.Fatal(err)
	}

	result, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Errorf("expected valid, got mismatches=%v missing=%v extra=%v",
			result.Mismatches, result.Missing, result.Extra)
	}
}

func TestVerify_TamperedFile(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	writeMetadata(t, dir, base, base.Add(99*time.Second), 100)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", makeEntries(100, base, "api"))
	writeIndex(t, dir, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(99 * time.Second), Lines: 100},
	})

	if _, err := Sign(dir); err != nil {
		t.Fatal(err)
	}

	// Tamper with index
	f, err := os.OpenFile(filepath.Join(dir, "index.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("tampered\n")
	_ = f.Close()

	result, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Error("expected invalid after tampering")
	}
	if len(result.Mismatches) != 1 {
		t.Errorf("expected 1 mismatch, got %d", len(result.Mismatches))
	}
	if result.Mismatches[0].File != "index.jsonl" {
		t.Errorf("expected index.jsonl mismatch, got %s", result.Mismatches[0].File)
	}
}

func TestVerify_MissingFile(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	writeMetadata(t, dir, base, base.Add(99*time.Second), 100)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", makeEntries(100, base, "api"))
	writeIndex(t, dir, []rotate.IndexEntry{
		{File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(99 * time.Second), Lines: 100},
	})

	if _, err := Sign(dir); err != nil {
		t.Fatal(err)
	}

	_ = os.Remove(filepath.Join(dir, "2024-01-15T100000-000.jsonl"))

	result, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Error("expected invalid after deleting file")
	}
	if len(result.Missing) != 1 {
		t.Errorf("expected 1 missing, got %d", len(result.Missing))
	}
}

func TestVerify_ExtraFile(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	writeMetadata(t, dir, base, base, 0)

	if _, err := Sign(dir); err != nil {
		t.Fatal(err)
	}

	// Add an extra file after signing
	_ = os.WriteFile(filepath.Join(dir, "extra.jsonl"), []byte("extra data\n"), 0o600)

	result, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Error("expected invalid with extra file")
	}
	if len(result.Extra) != 1 {
		t.Errorf("expected 1 extra, got %d", len(result.Extra))
	}
}

func TestVerify_NoManifest(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	writeMetadata(t, dir, base, base, 0)

	_, err := Verify(dir)
	if err == nil {
		t.Error("expected error when manifest is missing")
	}
}
