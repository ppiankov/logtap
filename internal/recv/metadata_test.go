package recv

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteAndReadMetadata(t *testing.T) {
	dir := t.TempDir()
	meta := &Metadata{
		Version:    1,
		Format:     "jsonl",
		Started:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		TotalLines: 42,
		TotalBytes: 1024,
		LabelsSeen: []string{"app", "env"},
		Redaction: &RedactionInfo{
			Enabled:  true,
			Patterns: []string{"email", "credit_card"},
		},
	}

	if err := WriteMetadata(dir, meta); err != nil {
		t.Fatal(err)
	}

	// verify file exists with restrictive permissions
	info, err2 := os.Stat(filepath.Join(dir, "metadata.json"))
	if err2 != nil {
		t.Fatal("metadata.json not created")
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("metadata.json permissions = %o, want 0600", perm)
	}

	got, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}

	if got.Version != 1 {
		t.Errorf("version: got %d, want 1", got.Version)
	}
	if got.Format != "jsonl" {
		t.Errorf("format: got %q, want jsonl", got.Format)
	}
	if got.TotalLines != 42 {
		t.Errorf("lines: got %d, want 42", got.TotalLines)
	}
	if got.TotalBytes != 1024 {
		t.Errorf("bytes: got %d, want 1024", got.TotalBytes)
	}
	if got.Redaction == nil || !got.Redaction.Enabled {
		t.Error("redaction should be enabled")
	}
	if len(got.LabelsSeen) != 2 {
		t.Errorf("labels_seen: got %d, want 2", len(got.LabelsSeen))
	}
}

func TestReadMetadataMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadMetadata(dir)
	if err == nil {
		t.Error("expected error for missing metadata")
	}
}

func TestReadMetadataInvalid(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte("{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadMetadata(dir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWriteMetadataBadDir(t *testing.T) {
	err := WriteMetadata("/nonexistent/path/deep", &Metadata{})
	if err == nil {
		t.Error("expected error for bad directory")
	}
}

func TestMetadataWithoutRedaction(t *testing.T) {
	dir := t.TempDir()
	meta := &Metadata{
		Version: 1,
		Format:  "jsonl",
		Started: time.Now(),
	}

	if err := WriteMetadata(dir, meta); err != nil {
		t.Fatal(err)
	}

	got, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Redaction != nil {
		t.Error("redaction should be nil when not set")
	}
}

func TestMetadataStoppedField(t *testing.T) {
	dir := t.TempDir()
	stopped := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)
	meta := &Metadata{
		Version: 1,
		Format:  "jsonl",
		Started: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Stopped: stopped,
	}

	if err := WriteMetadata(dir, meta); err != nil {
		t.Fatal(err)
	}

	got, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stopped.Equal(stopped) {
		t.Errorf("stopped: got %v, want %v", got.Stopped, stopped)
	}
}
