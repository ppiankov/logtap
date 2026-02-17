package cloud

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	gstorage "cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// mockGCSWriter is a mock io.WriteCloser for GCS upload tests.
type mockGCSWriter struct {
	buf      bytes.Buffer
	writeErr error
	closeErr error
}

func (m *mockGCSWriter) Write(p []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return m.buf.Write(p)
}

func (m *mockGCSWriter) Close() error {
	return m.closeErr
}

// mockGCSIterator implements gcsObjectIterator for testing.
type mockGCSIterator struct {
	objects []*gstorage.ObjectAttrs
	idx     int
	err     error
}

func (m *mockGCSIterator) Next() (*gstorage.ObjectAttrs, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.idx >= len(m.objects) {
		return nil, iterator.Done
	}
	obj := m.objects[m.idx]
	m.idx++
	return obj, nil
}

func newTestGCSBackend(
	writer *mockGCSWriter,
	readerBody string,
	readerErr error,
	iter gcsObjectIterator,
) *gcsBackend {
	return &gcsBackend{
		bucket: "test-bucket",
		newWriter: func(_ context.Context, _, _ string) io.WriteCloser {
			return writer
		},
		newReader: func(_ context.Context, _, _ string) (io.ReadCloser, error) {
			if readerErr != nil {
				return nil, readerErr
			}
			return io.NopCloser(strings.NewReader(readerBody)), nil
		},
		newIterator: func(_ context.Context, _, _ string) gcsObjectIterator {
			return iter
		},
	}
}

func TestGCSUpload_Success(t *testing.T) {
	w := &mockGCSWriter{}
	b := newTestGCSBackend(w, "", nil, nil)
	err := b.Upload(context.Background(), "key.txt", strings.NewReader("hello"), 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.buf.String() != "hello" {
		t.Errorf("written = %q, want %q", w.buf.String(), "hello")
	}
}

func TestGCSUpload_CopyError(t *testing.T) {
	w := &mockGCSWriter{writeErr: errors.New("write failed")}
	b := newTestGCSBackend(w, "", nil, nil)
	err := b.Upload(context.Background(), "key.txt", strings.NewReader("hello"), 5)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "gcs upload") {
		t.Errorf("error = %q, want to contain 'gcs upload'", err)
	}
}

func TestGCSUpload_CloseError(t *testing.T) {
	w := &mockGCSWriter{closeErr: errors.New("finalize failed")}
	b := newTestGCSBackend(w, "", nil, nil)
	err := b.Upload(context.Background(), "key.txt", strings.NewReader("hello"), 5)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "gcs finalize") {
		t.Errorf("error = %q, want to contain 'gcs finalize'", err)
	}
}

func TestGCSDownload_Success(t *testing.T) {
	b := newTestGCSBackend(nil, "file contents", nil, nil)
	var buf bytes.Buffer
	err := b.Download(context.Background(), "key.txt", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != "file contents" {
		t.Errorf("got %q, want %q", buf.String(), "file contents")
	}
}

func TestGCSDownload_GetError(t *testing.T) {
	b := newTestGCSBackend(nil, "", errors.New("not found"), nil)
	var buf bytes.Buffer
	err := b.Download(context.Background(), "key.txt", &buf)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "gcs get") {
		t.Errorf("error = %q, want to contain 'gcs get'", err)
	}
}

func TestGCSDownload_CopyError(t *testing.T) {
	// Inject a reader that fails partway.
	b := &gcsBackend{
		bucket: "test-bucket",
		newReader: func(_ context.Context, _, _ string) (io.ReadCloser, error) {
			return io.NopCloser(&failReader{}), nil
		},
	}
	var buf bytes.Buffer
	err := b.Download(context.Background(), "key.txt", &buf)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "gcs download") {
		t.Errorf("error = %q, want to contain 'gcs download'", err)
	}
}

func TestGCSList_Success(t *testing.T) {
	iter := &mockGCSIterator{
		objects: []*gstorage.ObjectAttrs{
			{Name: "prefix/file1.txt", Size: 100},
			{Name: "prefix/file2.txt", Size: 200},
		},
	}
	b := newTestGCSBackend(nil, "", nil, iter)
	objects, err := b.List(context.Background(), "prefix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 2 {
		t.Fatalf("got %d objects, want 2", len(objects))
	}
	if objects[0].Key != "prefix/file1.txt" || objects[0].Size != 100 {
		t.Errorf("objects[0] = %+v", objects[0])
	}
	if objects[1].Key != "prefix/file2.txt" || objects[1].Size != 200 {
		t.Errorf("objects[1] = %+v", objects[1])
	}
}

func TestGCSList_Error(t *testing.T) {
	iter := &mockGCSIterator{err: errors.New("list failed")}
	b := newTestGCSBackend(nil, "", nil, iter)
	_, err := b.List(context.Background(), "prefix")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "gcs list") {
		t.Errorf("error = %q, want to contain 'gcs list'", err)
	}
}

func TestGCSList_EmptyResult(t *testing.T) {
	iter := &mockGCSIterator{objects: nil} // empty — Next() returns iterator.Done immediately
	b := newTestGCSBackend(nil, "", nil, iter)
	objects, err := b.List(context.Background(), "prefix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 0 {
		t.Fatalf("got %d objects, want 0", len(objects))
	}
}

func TestGCSList_EmptyPrefix(t *testing.T) {
	iter := &mockGCSIterator{
		objects: []*gstorage.ObjectAttrs{
			{Name: "root.txt", Size: 10},
		},
	}
	b := newTestGCSBackend(nil, "", nil, iter)
	objects, err := b.List(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("got %d objects, want 1", len(objects))
	}
}

func TestNewGCSBackend_BadCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent/creds.json")
	_, err := newGCSBackend(context.Background(), "test-bucket")
	if err == nil {
		t.Skip("GCS client creation succeeded despite bad credentials path")
	}
	if !strings.Contains(err.Error(), "create GCS client") {
		t.Errorf("error = %q, want to contain 'create GCS client'", err)
	}
}

func TestGCSList_PrefixWithTrailingSlash(t *testing.T) {
	iter := &mockGCSIterator{
		objects: []*gstorage.ObjectAttrs{
			{Name: "prefix/file.txt", Size: 10},
		},
	}
	b := newTestGCSBackend(nil, "", nil, iter)
	objects, err := b.List(context.Background(), "prefix/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("got %d objects, want 1", len(objects))
	}
}
