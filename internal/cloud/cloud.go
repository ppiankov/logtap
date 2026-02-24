package cloud

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Backend abstracts cloud object storage operations.
type Backend interface {
	// Upload writes the content from r to the given key.
	Upload(ctx context.Context, key string, r io.Reader, size int64) error

	// Download reads the object at key and writes it to w.
	Download(ctx context.Context, key string, w io.Writer) error

	// List returns all object keys under the given prefix.
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)

	// ShareURL generates a time-limited presigned URL for downloading the given key.
	ShareURL(ctx context.Context, key string, expiry time.Duration) (string, error)
}

// ObjectInfo describes a remote object.
type ObjectInfo struct {
	Key  string
	Size int64
}

// ParseURL extracts scheme, bucket, and prefix from a cloud URL.
// Supported schemes: s3://, gs://
func ParseURL(raw string) (scheme, bucket, prefix string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", fmt.Errorf("empty URL")
	}

	var rest string
	switch {
	case strings.HasPrefix(raw, "s3://"):
		scheme = "s3"
		rest = strings.TrimPrefix(raw, "s3://")
	case strings.HasPrefix(raw, "gs://"):
		scheme = "gs"
		rest = strings.TrimPrefix(raw, "gs://")
	default:
		return "", "", "", fmt.Errorf("unsupported scheme in %q: expected s3:// or gs://", raw)
	}

	if rest == "" {
		return "", "", "", fmt.Errorf("empty bucket in %q", raw)
	}

	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		bucket = rest
		return scheme, bucket, "", nil
	}

	bucket = rest[:idx]
	if bucket == "" {
		return "", "", "", fmt.Errorf("empty bucket in %q", raw)
	}
	prefix = strings.TrimSuffix(rest[idx+1:], "/")

	return scheme, bucket, prefix, nil
}

// NewBackend creates a Backend for the given scheme and bucket.
func NewBackend(ctx context.Context, scheme, bucket string) (Backend, error) {
	switch scheme {
	case "s3":
		return newS3Backend(ctx, bucket)
	case "gs":
		return newGCSBackend(ctx, bucket)
	default:
		return nil, fmt.Errorf("unsupported scheme %q: expected s3 or gs", scheme)
	}
}
