package cloud

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3Backend struct {
	client *s3.Client
	bucket string
}

func newS3Backend(ctx context.Context, bucket string) (*s3Backend, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return &s3Backend{client: s3.NewFromConfig(cfg), bucket: bucket}, nil
}

func (b *s3Backend) Upload(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &b.bucket,
		Key:           &key,
		Body:          r,
		ContentLength: &size,
	})
	if err != nil {
		return fmt.Errorf("s3 upload %s: %w", key, err)
	}
	return nil
}

func (b *s3Backend) Download(ctx context.Context, key string, w io.Writer) error {
	resp, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &b.bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("s3 get %s: %w", key, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("s3 download %s: %w", key, err)
	}
	return nil
}

func (b *s3Backend) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	listPrefix := prefix
	if listPrefix != "" && !strings.HasSuffix(listPrefix, "/") {
		listPrefix += "/"
	}

	var objects []ObjectInfo
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: &b.bucket,
		Prefix: &listPrefix,
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			var size int64
			if obj.Size != nil {
				size = *obj.Size
			}
			objects = append(objects, ObjectInfo{Key: *obj.Key, Size: size})
		}
	}

	return objects, nil
}
