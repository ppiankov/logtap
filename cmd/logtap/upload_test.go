package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/cloud"
	"github.com/ppiankov/logtap/internal/recv"
)

type mockBackend struct {
	mu          sync.Mutex
	uploads     []mockUpload
	objects     []cloud.ObjectInfo
	data        map[string][]byte
	uploadErr   error
	downloadErr error
	listErr     error
	shareURLErr error
}

type mockUpload struct {
	Key  string
	Data []byte
	Size int64
}

func (m *mockBackend) Upload(_ context.Context, key string, r io.Reader, size int64) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.uploads = append(m.uploads, mockUpload{Key: key, Data: data, Size: size})
	m.mu.Unlock()
	return nil
}

func (m *mockBackend) Download(_ context.Context, key string, w io.Writer) error {
	if m.downloadErr != nil {
		return m.downloadErr
	}
	data, ok := m.data[key]
	if !ok {
		return fmt.Errorf("object not found: %s", key)
	}
	_, err := w.Write(data)
	return err
}

func (m *mockBackend) List(_ context.Context, _ string) ([]cloud.ObjectInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.objects, nil
}

func (m *mockBackend) ShareURL(_ context.Context, key string, _ time.Duration) (string, error) {
	if m.shareURLErr != nil {
		return "", m.shareURLErr
	}
	return "https://presigned.example.com/" + key, nil
}

func TestUploadCapture(t *testing.T) {
	dir := makeMinimalCapture(t)

	mock := &mockBackend{data: make(map[string][]byte)}
	_, err := uploadCapture(context.Background(), dir, mock, "captures/test", 2)
	if err != nil {
		t.Fatalf("uploadCapture error: %v", err)
	}

	if len(mock.uploads) < 2 {
		t.Fatalf("expected at least 2 uploads (metadata + index), got %d", len(mock.uploads))
	}

	keys := make(map[string]bool)
	for _, u := range mock.uploads {
		keys[u.Key] = true
	}
	if !keys["captures/test/metadata.json"] {
		t.Error("expected metadata.json upload")
	}
	if !keys["captures/test/index.jsonl"] {
		t.Error("expected index.jsonl upload")
	}
}

func TestUploadCapture_NoPrefix(t *testing.T) {
	dir := makeMinimalCapture(t)

	mock := &mockBackend{data: make(map[string][]byte)}
	_, err := uploadCapture(context.Background(), dir, mock, "", 1)
	if err != nil {
		t.Fatalf("uploadCapture error: %v", err)
	}

	keys := make(map[string]bool)
	for _, u := range mock.uploads {
		keys[u.Key] = true
	}
	if !keys["metadata.json"] {
		t.Error("expected metadata.json without prefix")
	}
}

func TestUploadCapture_NotCaptureDir(t *testing.T) {
	dir := t.TempDir()
	// No metadata.json — runUpload validates this
	err := runUpload(context.Background(), dir, "s3://bucket/prefix", 1, false, false, 24*time.Hour, false)
	if err == nil {
		t.Fatal("expected error for non-capture dir")
	}
}

func TestUploadCapture_UploadError(t *testing.T) {
	dir := makeMinimalCapture(t)

	mock := &mockBackend{
		data:      make(map[string][]byte),
		uploadErr: fmt.Errorf("connection refused"),
	}
	_, err := uploadCapture(context.Background(), dir, mock, "prefix", 1)
	if err == nil {
		t.Fatal("expected error on upload failure")
	}
}

func TestUploadCapture_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	mock := &mockBackend{data: make(map[string][]byte)}
	_, err := uploadCapture(context.Background(), dir, mock, "prefix", 1)
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestUploadShare_UnredactedRefused(t *testing.T) {
	dir := makeMinimalCapture(t) // no redaction in metadata

	// runUpload should refuse --share without --force on unredacted capture
	err := runUpload(context.Background(), dir, "s3://bucket/prefix", 1, false, true, 24*time.Hour, false)
	if err == nil {
		t.Fatal("expected error for unredacted share without --force")
	}
	if !strings.Contains(err.Error(), "not redacted") {
		t.Errorf("error = %q, want to contain 'not redacted'", err)
	}
}

func TestUploadShare_RedactedAllowed(t *testing.T) {
	dir := makeRedactedCapture(t)

	// runUpload with --share on redacted capture should NOT error on safety gate
	// (will fail on cloud connect, which is fine — we're testing the safety gate only)
	err := runUpload(context.Background(), dir, "s3://bucket/prefix", 1, false, true, 24*time.Hour, false)
	if err == nil {
		t.Skip("unexpected success — cloud connect might have worked")
	}
	// should NOT be the redaction error
	if strings.Contains(err.Error(), "not redacted") {
		t.Errorf("redacted capture should pass safety gate, got: %v", err)
	}
}

func TestUploadShare_ExpiryTooLong(t *testing.T) {
	dir := makeRedactedCapture(t)

	err := runUpload(context.Background(), dir, "s3://bucket/prefix", 1, false, true, 200*time.Hour, false)
	if err == nil {
		t.Fatal("expected error for expiry > 168h")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("error = %q, want to contain 'exceeds maximum'", err)
	}
}

func makeMinimalCapture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	meta := recv.Metadata{
		Version:    1,
		Format:     "jsonl",
		TotalLines: 10,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data-000.jsonl"), []byte(`{"msg":"test"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func makeRedactedCapture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	meta := recv.Metadata{
		Version:    1,
		Format:     "jsonl",
		TotalLines: 10,
		Redaction: &recv.RedactionInfo{
			Enabled:  true,
			Patterns: []string{"email", "credit_card"},
		},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
