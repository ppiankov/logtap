package cloud

import (
	"context"
	"fmt"
	"io"

	gstorage "cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

type gcsBackend struct {
	client *gstorage.Client
	bucket string
}

func newGCSBackend(ctx context.Context, bucket string) (*gcsBackend, error) {
	client, err := gstorage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCS client: %w", err)
	}
	return &gcsBackend{client: client, bucket: bucket}, nil
}

func (b *gcsBackend) Upload(ctx context.Context, key string, r io.Reader, size int64) error {
	obj := b.client.Bucket(b.bucket).Object(key)
	w := obj.NewWriter(ctx)
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
	obj := b.client.Bucket(b.bucket).Object(key)
	r, err := obj.NewReader(ctx)
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
	it := b.client.Bucket(b.bucket).Objects(ctx, &gstorage.Query{Prefix: listPrefix})
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
