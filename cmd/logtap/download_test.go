package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppiankov/logtap/internal/cloud"
	"github.com/ppiankov/logtap/internal/recv"
)

func TestDownloadCapture(t *testing.T) {
	meta := recv.Metadata{Version: 1, Format: "jsonl", TotalLines: 5}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockBackend{
		objects: []cloud.ObjectInfo{
			{Key: "captures/test/metadata.json", Size: int64(len(metaBytes))},
			{Key: "captures/test/index.jsonl", Size: 3},
			{Key: "captures/test/data-000.jsonl", Size: 15},
		},
		data: map[string][]byte{
			"captures/test/metadata.json":  metaBytes,
			"captures/test/index.jsonl":    []byte("{}\n"),
			"captures/test/data-000.jsonl": []byte(`{"msg":"hello"}` + "\n"),
		},
	}

	outDir := filepath.Join(t.TempDir(), "output")
	_, err = downloadCapture(context.Background(), mock, "captures/test", outDir, 2)
	if err != nil {
		t.Fatalf("downloadCapture error: %v", err)
	}

	// Verify files exist
	for _, name := range []string{"metadata.json", "index.jsonl", "data-000.jsonl"} {
		path := filepath.Join(outDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	// Verify metadata is valid
	if _, err := recv.ReadMetadata(outDir); err != nil {
		t.Errorf("downloaded metadata invalid: %v", err)
	}
}

func TestDownloadCapture_EmptyList(t *testing.T) {
	mock := &mockBackend{
		objects: nil,
		data:    make(map[string][]byte),
	}

	outDir := filepath.Join(t.TempDir(), "output")
	_, err := downloadCapture(context.Background(), mock, "prefix", outDir, 1)
	if err == nil {
		t.Fatal("expected error for empty object list")
	}
}

func TestDownloadCapture_DownloadError(t *testing.T) {
	mock := &mockBackend{
		objects: []cloud.ObjectInfo{
			{Key: "prefix/metadata.json", Size: 10},
		},
		data:        make(map[string][]byte),
		downloadErr: fmt.Errorf("permission denied"),
	}

	outDir := filepath.Join(t.TempDir(), "output")
	_, err := downloadCapture(context.Background(), mock, "prefix", outDir, 1)
	if err == nil {
		t.Fatal("expected error on download failure")
	}
}

func TestDownloadCapture_InvalidMetadata(t *testing.T) {
	mock := &mockBackend{
		objects: []cloud.ObjectInfo{
			{Key: "prefix/metadata.json", Size: 11},
		},
		data: map[string][]byte{
			"prefix/metadata.json": []byte("not json!!!"),
		},
	}

	outDir := filepath.Join(t.TempDir(), "output")
	_, err := downloadCapture(context.Background(), mock, "prefix", outDir, 1)
	if err == nil {
		t.Fatal("expected error for invalid metadata")
	}
}

func TestDownloadCapture_ListError(t *testing.T) {
	mock := &mockBackend{
		listErr: fmt.Errorf("access denied"),
	}

	outDir := filepath.Join(t.TempDir(), "output")
	_, err := downloadCapture(context.Background(), mock, "prefix", outDir, 1)
	if err == nil {
		t.Fatal("expected error on list failure")
	}
}

func TestDownloadCapture_PathTraversal(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"dot-dot prefix", "prefix/../../../etc/passwd"},
		{"dot-dot mid-path", "prefix/sub/../../.ssh/authorized_keys"},
		{"absolute path", "prefix//etc/shadow"},
		{"dot-dot after strip", "prefix/../outside"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBackend{
				objects: []cloud.ObjectInfo{
					{Key: tt.key, Size: 5},
				},
				data: map[string][]byte{
					tt.key: []byte("pwned"),
				},
			}

			outDir := filepath.Join(t.TempDir(), "output")
			_, err := downloadCapture(context.Background(), mock, "prefix", outDir, 1)
			if err == nil {
				t.Fatal("expected error for path traversal key")
			}
			if !strings.Contains(err.Error(), "unsafe object key") {
				t.Errorf("expected 'unsafe object key' error, got: %v", err)
			}
		})
	}
}

func TestStripPrefix(t *testing.T) {
	tests := []struct {
		key    string
		prefix string
		want   string
	}{
		{"captures/test/metadata.json", "captures/test", "metadata.json"},
		{"captures/test/sub/data.jsonl", "captures/test", "sub/data.jsonl"},
		{"metadata.json", "", "metadata.json"},
		{"prefix/file.txt", "prefix", "file.txt"},
		{"prefix/deep/nested/file.txt", "prefix", "deep/nested/file.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := stripPrefix(tt.key, tt.prefix)
			if got != tt.want {
				t.Errorf("stripPrefix(%q, %q) = %q, want %q", tt.key, tt.prefix, got, tt.want)
			}
		})
	}
}
