package cloud

import (
	"context"
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
