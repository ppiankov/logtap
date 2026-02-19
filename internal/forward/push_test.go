package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestPush_Success(t *testing.T) {
	var received lokiPushRequest
	var decodeErr error
	var gotMethod string
	var gotPath string
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			decodeErr = json.NewDecoder(r.Body).Decode(&received)
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	p := NewPusherWithClient("receiver:3100", client)

	labels := map[string]string{"namespace": "default", "pod": "api-gw-abc"}
	lines := []TimestampedLine{
		{Timestamp: time.Unix(0, 1000000000), Line: "hello world"},
		{Timestamp: time.Unix(0, 2000000000), Line: "second line"},
	}

	err := p.Push(context.Background(), labels, lines)
	if err != nil {
		t.Fatal(err)
	}
	if decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if !strings.HasSuffix(gotPath, pushPath) {
		t.Errorf("path = %s, want %s", gotPath, pushPath)
	}

	if len(received.Streams) != 1 {
		t.Fatalf("streams = %d, want 1", len(received.Streams))
	}
	if len(received.Streams[0].Values) != 2 {
		t.Fatalf("values = %d, want 2", len(received.Streams[0].Values))
	}
	if received.Streams[0].Values[0][1] != "hello world" {
		t.Errorf("line = %q, want %q", received.Streams[0].Values[0][1], "hello world")
	}
	if received.Streams[0].Stream["namespace"] != "default" {
		t.Errorf("label namespace = %q, want %q", received.Streams[0].Stream["namespace"], "default")
	}
}

func TestPush_ServerError(t *testing.T) {
	calls := 0
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	p := NewPusherWithClient("receiver:3100", client)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := p.Push(ctx, map[string]string{"pod": "test"}, []TimestampedLine{
		{Timestamp: time.Now(), Line: "test"},
	})

	if err == nil {
		t.Fatal("expected error for server error")
	}
	if calls != defaultMaxRetries {
		t.Errorf("calls = %d, want %d (should retry)", calls, defaultMaxRetries)
	}
}

func TestPush_BufferLimit(t *testing.T) {
	p := NewPusher("localhost:9999")

	// generate a line larger than 1MB
	bigLine := strings.Repeat("x", maxBufferBytes+1)
	lines := []TimestampedLine{
		{Timestamp: time.Now(), Line: bigLine},
	}

	err := p.Push(context.Background(), map[string]string{}, lines)
	if err != ErrBufferExceeded {
		t.Errorf("err = %v, want ErrBufferExceeded", err)
	}
}

func TestPush_EmptyLines(t *testing.T) {
	p := NewPusher("localhost:9999")
	err := p.Push(context.Background(), map[string]string{}, nil)
	if err != nil {
		t.Errorf("expected nil for empty lines, got %v", err)
	}
}

func TestPush_ConnectionError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		}),
	}
	p := NewPusherWithClient("receiver:3100", client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := p.Push(ctx, map[string]string{"pod": "test"}, []TimestampedLine{
		{Timestamp: time.Now(), Line: "test"},
	})
	if err == nil {
		t.Fatal("expected error for closed server")
	}
}

func TestPush_ClientError(t *testing.T) {
	calls := 0
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	p := NewPusherWithClient("receiver:3100", client)

	err := p.Push(context.Background(), map[string]string{"pod": "test"}, []TimestampedLine{
		{Timestamp: time.Now(), Line: "test"},
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (should not retry client errors)", calls)
	}
}

func TestPush_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p := NewPusher("localhost:9999")
	err := p.Push(ctx, map[string]string{"pod": "test"}, []TimestampedLine{
		{Timestamp: time.Now(), Line: "test"},
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestBackoff_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	backoff(ctx, 5, defaultMaxBackoff) // attempt 5 would normally sleep 32s
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("backoff took %v, expected immediate return on cancelled context", elapsed)
	}
}

func TestPush_MaxBackoffCap(t *testing.T) {
	calls := 0
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	p := NewPusherWithClient("receiver:3100", client)
	p.SetMaxRetries(5)
	p.SetMaxBackoff(10 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	_ = p.Push(ctx, map[string]string{"pod": "test"}, []TimestampedLine{
		{Timestamp: time.Now(), Line: "test"},
	})
	elapsed := time.Since(start)

	if calls != 5 {
		t.Errorf("calls = %d, want 5", calls)
	}
	// with 10ms max backoff, 4 retries should complete in well under 1s
	if elapsed > 1*time.Second {
		t.Errorf("took %v, expected fast completion with low max backoff", elapsed)
	}
}

func TestPush_OnRetryCallback(t *testing.T) {
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		}),
	}
	p := NewPusherWithClient("receiver:3100", client)
	p.SetMaxRetries(4)
	p.SetMaxBackoff(1 * time.Millisecond)

	retryCount := 0
	p.SetOnRetry(func() { retryCount++ })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = p.Push(ctx, map[string]string{"pod": "test"}, []TimestampedLine{
		{Timestamp: time.Now(), Line: "test"},
	})

	// 4 attempts = 3 retries (first attempt is not a retry)
	if retryCount != 3 {
		t.Errorf("retryCount = %d, want 3", retryCount)
	}
}

func TestBuildPushURL(t *testing.T) {
	tests := []struct {
		target string
		want   string
	}{
		{"receiver:3100", "http://receiver:3100/loki/api/v1/push"},
		{"http://receiver:3100", "http://receiver:3100/loki/api/v1/push"},
		{"https://receiver:3100", "https://receiver:3100/loki/api/v1/push"},
		{"https://receiver:3100/", "https://receiver:3100/loki/api/v1/push"},
	}
	for _, tt := range tests {
		got := buildPushURL(tt.target)
		if got != tt.want {
			t.Errorf("buildPushURL(%q) = %q, want %q", tt.target, got, tt.want)
		}
	}
}

func TestTargetURL(t *testing.T) {
	tests := []struct {
		target, path, want string
	}{
		{"receiver:3100", "/metrics", "http://receiver:3100/metrics"},
		{"https://receiver:3100", "/metrics", "https://receiver:3100/metrics"},
		{"http://receiver:3100", "/healthz", "http://receiver:3100/healthz"},
	}
	for _, tt := range tests {
		got := TargetURL(tt.target, tt.path)
		if got != tt.want {
			t.Errorf("TargetURL(%q, %q) = %q, want %q", tt.target, tt.path, got, tt.want)
		}
	}
}

func TestPush_HTTPSTarget(t *testing.T) {
	var gotScheme string
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			gotScheme = r.URL.Scheme
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	p := NewPusherWithClient("https://receiver:3100", client)
	err := p.Push(context.Background(), map[string]string{"pod": "test"}, []TimestampedLine{
		{Timestamp: time.Now(), Line: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotScheme != "https" {
		t.Errorf("scheme = %q, want %q", gotScheme, "https")
	}
}

func TestNewTLSPusher(t *testing.T) {
	p := NewTLSPusher("https://receiver:3100", true)
	if p == nil {
		t.Fatal("expected non-nil pusher")
	}
	if p.target != "https://receiver:3100" {
		t.Errorf("target = %q, want %q", p.target, "https://receiver:3100")
	}
}
