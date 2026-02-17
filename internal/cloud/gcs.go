package cloud

import (
	"context"
	"fmt"
	"io"

	gstorage "cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// gcsObjectIterator abstracts the GCS object iterator.
type gcsObjectIterator interface {
	Next() (*gstorage.ObjectAttrs, error)
}

type gcsBackend struct {
	bucket      string
	newWriter   func(ctx context.Context, bucket, key string) io.WriteCloser
	newReader   func(ctx context.Context, bucket, key string) (io.ReadCloser, error)
	newIterator func(ctx context.Context, bucket, prefix string) gcsObjectIterator
}

func newGCSBackend(ctx context.Context, bucket string) (*gcsBackend, error) {
	client, err := gstorage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCS client: %w", err)
	}
	return &gcsBackend{
		bucket: bucket,
		newWriter: func(ctx context.Context, b, key string) io.WriteCloser {
			return client.Bucket(b).Object(key).NewWriter(ctx)
		},
		newReader: func(ctx context.Context, b, key string) (io.ReadCloser, error) {
			return client.Bucket(b).Object(key).NewReader(ctx)
		},
		newIterator: func(ctx context.Context, b, prefix string) gcsObjectIterator {
			return client.Bucket(b).Objects(ctx, &gstorage.Query{Prefix: prefix})
		},
	}, nil
}

func (b *gcsBackend) Upload(ctx context.Context, key string, r io.Reader, _ int64) error {
	w := b.newWriter(ctx, b.bucket, key)
	if _, err := io.Copy(w, r); err != nil {
		_ = w.Close()
		return fmt.Errorf("gcs upload %s: %w", key, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("gcs finalize %s: %w", key, err)
	}
	return nil
}

func (b *gcsBackend) Download(ctx context.Context, key string, w io.Writer) error {
	r, err := b.newReader(ctx, b.bucket, key)
	if err != nil {
		return fmt.Errorf("gcs get %s: %w", key, err)
	}
	defer func() { _ = r.Close() }()
	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("gcs download %s: %w", key, err)
	}
	return nil
}

func (b *gcsBackend) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	listPrefix := prefix
	if listPrefix != "" && listPrefix[len(listPrefix)-1] != '/' {
		listPrefix += "/"
	}

	var objects []ObjectInfo
	it := b.newIterator(ctx, b.bucket, listPrefix)
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gcs list: %w", err)
		}
		objects = append(objects, ObjectInfo{Key: attrs.Name, Size: attrs.Size})
	}

	return objects, nil
}
