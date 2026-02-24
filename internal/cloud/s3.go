package cloud

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3API abstracts the S3 client methods used by s3Backend.
type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// s3Paginator abstracts the S3 list paginator.
type s3Paginator interface {
	HasMorePages() bool
	NextPage(ctx context.Context, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

type s3Backend struct {
	client       s3API
	bucket       string
	newPaginator func(client s3API, bucket, prefix string) s3Paginator
	presignURL   func(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)
}

func newS3Backend(ctx context.Context, bucket string) (*s3Backend, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	client := s3.NewFromConfig(cfg)
	presigner := s3.NewPresignClient(client)
	return &s3Backend{
		client: client,
		bucket: bucket,
		newPaginator: func(c s3API, b, p string) s3Paginator {
			return s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
				Bucket: &b,
				Prefix: &p,
			})
		},
		presignURL: func(ctx context.Context, b, key string, expiry time.Duration) (string, error) {
			result, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
				Bucket: &b,
				Key:    &key,
			}, s3.WithPresignExpires(expiry))
			if err != nil {
				return "", err
			}
			return result.URL, nil
		},
	}, nil
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
	paginator := b.newPaginator(b.client, b.bucket, listPrefix)

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

func (b *s3Backend) ShareURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	url, err := b.presignURL(ctx, b.bucket, key, expiry)
	if err != nil {
		return "", fmt.Errorf("s3 presign %s: %w", key, err)
	}
	return url, nil
}
