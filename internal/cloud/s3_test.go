package cloud

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// mockS3Client implements s3API for testing.
type mockS3Client struct {
	putErr  error
	getBody string
	getErr  error
}

func (m *mockS3Client) PutObject(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return &s3.PutObjectOutput{}, m.putErr
}

func (m *mockS3Client) GetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(strings.NewReader(m.getBody)),
	}, nil
}

// mockPaginator implements s3Paginator for testing.
type mockPaginator struct {
	pages []*s3.ListObjectsV2Output
	idx   int
	err   error
}

func (m *mockPaginator) HasMorePages() bool {
	return m.idx < len(m.pages)
}

func (m *mockPaginator) NextPage(_ context.Context, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if m.err != nil {
		return nil, m.err
	}
	page := m.pages[m.idx]
	m.idx++
	return page, nil
}

func newTestS3Backend(client s3API, pag s3Paginator) *s3Backend {
	return &s3Backend{
		client: client,
		bucket: "test-bucket",
		newPaginator: func(_ s3API, _, _ string) s3Paginator {
			return pag
		},
	}
}

func TestS3Upload_Success(t *testing.T) {
	b := newTestS3Backend(&mockS3Client{}, nil)
	err := b.Upload(context.Background(), "key.txt", strings.NewReader("hello"), 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestS3Upload_Error(t *testing.T) {
	b := newTestS3Backend(&mockS3Client{putErr: errors.New("access denied")}, nil)
	err := b.Upload(context.Background(), "key.txt", strings.NewReader("hello"), 5)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "s3 upload") {
		t.Errorf("error = %q, want to contain 's3 upload'", err)
	}
}

func TestS3Download_Success(t *testing.T) {
	b := newTestS3Backend(&mockS3Client{getBody: "file contents"}, nil)
	var buf bytes.Buffer
	err := b.Download(context.Background(), "key.txt", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != "file contents" {
		t.Errorf("got %q, want %q", buf.String(), "file contents")
	}
}

func TestS3Download_GetError(t *testing.T) {
	b := newTestS3Backend(&mockS3Client{getErr: errors.New("not found")}, nil)
	var buf bytes.Buffer
	err := b.Download(context.Background(), "key.txt", &buf)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "s3 get") {
		t.Errorf("error = %q, want to contain 's3 get'", err)
	}
}

func TestS3Download_CopyError(t *testing.T) {
	// Return a reader that fails partway through.
	client := &mockS3Client{}
	b := newTestS3Backend(client, nil)
	// Override GetObject to return a failing reader.
	failClient := &failingReadS3Client{}
	b.client = failClient

	var buf bytes.Buffer
	err := b.Download(context.Background(), "key.txt", &buf)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "s3 download") {
		t.Errorf("error = %q, want to contain 's3 download'", err)
	}
}

// failingReadS3Client returns a reader that fails after a few bytes.
type failingReadS3Client struct{}

func (f *failingReadS3Client) PutObject(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return &s3.PutObjectOutput{}, nil
}

func (f *failingReadS3Client) GetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{
		Body: io.NopCloser(&failReader{}),
	}, nil
}

type failReader struct{ n int }

func (f *failReader) Read(p []byte) (int, error) {
	if f.n > 0 {
		return 0, errors.New("read failure")
	}
	f.n++
	copy(p, "partial")
	return 7, nil
}

func TestS3List_Success(t *testing.T) {
	key1 := "prefix/file1.txt"
	key2 := "prefix/file2.txt"
	size1 := int64(100)
	size2 := int64(200)

	pag := &mockPaginator{
		pages: []*s3.ListObjectsV2Output{
			{Contents: []s3types.Object{
				{Key: &key1, Size: &size1},
			}},
			{Contents: []s3types.Object{
				{Key: &key2, Size: &size2},
			}},
		},
	}

	b := newTestS3Backend(&mockS3Client{}, pag)
	objects, err := b.List(context.Background(), "prefix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 2 {
		t.Fatalf("got %d objects, want 2", len(objects))
	}
	if objects[0].Key != key1 || objects[0].Size != 100 {
		t.Errorf("objects[0] = %+v, want key=%q size=100", objects[0], key1)
	}
	if objects[1].Key != key2 || objects[1].Size != 200 {
		t.Errorf("objects[1] = %+v, want key=%q size=200", objects[1], key2)
	}
}

func TestS3List_Error(t *testing.T) {
	pag := &mockPaginator{
		pages: []*s3.ListObjectsV2Output{{}}, // need at least one page to enter loop
		err:   errors.New("list failed"),
	}

	b := newTestS3Backend(&mockS3Client{}, pag)
	_, err := b.List(context.Background(), "prefix")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "s3 list") {
		t.Errorf("error = %q, want to contain 's3 list'", err)
	}
}

func TestS3List_NilKey(t *testing.T) {
	validKey := "prefix/valid.txt"
	size := int64(50)

	pag := &mockPaginator{
		pages: []*s3.ListObjectsV2Output{
			{Contents: []s3types.Object{
				{Key: nil, Size: &size},       // nil key — should be skipped
				{Key: &validKey, Size: &size}, // valid
				{Key: nil},                    // nil key and nil size
			}},
		},
	}

	b := newTestS3Backend(&mockS3Client{}, pag)
	objects, err := b.List(context.Background(), "prefix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("got %d objects, want 1 (nil keys should be skipped)", len(objects))
	}
	if objects[0].Key != validKey {
		t.Errorf("key = %q, want %q", objects[0].Key, validKey)
	}
}

func TestS3List_NilSize(t *testing.T) {
	key := "prefix/file.txt"

	pag := &mockPaginator{
		pages: []*s3.ListObjectsV2Output{
			{Contents: []s3types.Object{
				{Key: &key, Size: nil}, // nil size — should default to 0
			}},
		},
	}

	b := newTestS3Backend(&mockS3Client{}, pag)
	objects, err := b.List(context.Background(), "prefix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("got %d objects, want 1", len(objects))
	}
	if objects[0].Size != 0 {
		t.Errorf("size = %d, want 0 for nil size", objects[0].Size)
	}
}

func TestS3List_EmptyPrefix(t *testing.T) {
	key := "root.txt"
	size := int64(10)

	pag := &mockPaginator{
		pages: []*s3.ListObjectsV2Output{
			{Contents: []s3types.Object{
				{Key: &key, Size: &size},
			}},
		},
	}

	b := newTestS3Backend(&mockS3Client{}, pag)
	objects, err := b.List(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("got %d objects, want 1", len(objects))
	}
}

func TestS3List_PrefixWithTrailingSlash(t *testing.T) {
	key := "prefix/file.txt"
	size := int64(10)

	pag := &mockPaginator{
		pages: []*s3.ListObjectsV2Output{
			{Contents: []s3types.Object{
				{Key: &key, Size: &size},
			}},
		},
	}

	b := newTestS3Backend(&mockS3Client{}, pag)
	// Prefix already has trailing slash — should not be doubled
	objects, err := b.List(context.Background(), "prefix/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("got %d objects, want 1", len(objects))
	}
}

func TestS3List_EmptyResult(t *testing.T) {
	pag := &mockPaginator{
		pages: []*s3.ListObjectsV2Output{
			{Contents: nil},
		},
	}

	b := newTestS3Backend(&mockS3Client{}, pag)
	objects, err := b.List(context.Background(), "empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 0 {
		t.Fatalf("got %d objects, want 0", len(objects))
	}
}
