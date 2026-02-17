package cloud

import (
	"context"
	"strings"
	"testing"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		input   string
		scheme  string
		bucket  string
		prefix  string
		wantErr bool
	}{
		{"s3://my-bucket/path/to/prefix/", "s3", "my-bucket", "path/to/prefix", false},
		{"s3://my-bucket/path/to/prefix", "s3", "my-bucket", "path/to/prefix", false},
		{"gs://my-bucket/prefix", "gs", "my-bucket", "prefix", false},
		{"gs://my-bucket/deep/nested/prefix/", "gs", "my-bucket", "deep/nested/prefix", false},
		{"s3://bucket/", "s3", "bucket", "", false},
		{"s3://bucket", "s3", "bucket", "", false},
		{"gs://bucket", "gs", "bucket", "", false},
		{"  s3://bucket/path  ", "s3", "bucket", "path", false},
		{"http://invalid", "", "", "", true},
		{"ftp://bucket/path", "", "", "", true},
		{"", "", "", "", true},
		{"s3://", "", "", "", true},
		{"gs://", "", "", "", true},
		{"s3:///prefix", "", "", "", true},
		{"no-scheme", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			scheme, bucket, prefix, err := ParseURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if scheme != tt.scheme {
				t.Errorf("scheme = %q, want %q", scheme, tt.scheme)
			}
			if bucket != tt.bucket {
				t.Errorf("bucket = %q, want %q", bucket, tt.bucket)
			}
			if prefix != tt.prefix {
				t.Errorf("prefix = %q, want %q", prefix, tt.prefix)
			}
		})
	}
}

func TestNewBackendUnsupportedScheme(t *testing.T) {
	_, err := NewBackend(context.Background(), "ftp", "bucket")
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}

func TestNewBackendS3(t *testing.T) {
	// S3 backend creation succeeds even without real credentials —
	// config.LoadDefaultConfig returns empty config and s3.NewFromConfig
	// creates a client that will fail on actual API calls, not construction.
	b, err := NewBackend(context.Background(), "s3", "test-bucket")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestNewBackendS3_BucketName(t *testing.T) {
	b, err := newS3Backend(context.Background(), "my-special-bucket")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.bucket != "my-special-bucket" {
		t.Errorf("bucket = %q, want %q", b.bucket, "my-special-bucket")
	}
}

func TestNewBackend_GCSBadCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent/creds.json")
	_, err := NewBackend(context.Background(), "gs", "test-bucket")
	if err == nil {
		t.Skip("GCS client creation succeeded despite bad credentials path")
	}
	if !strings.Contains(err.Error(), "GCS") {
		t.Errorf("error = %q, want to contain 'GCS'", err)
	}
}
