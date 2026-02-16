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
	if calls != maxRetries {
		t.Errorf("calls = %d, want %d (should retry)", calls, maxRetries)
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
	backoff(ctx, 5) // attempt 5 would normally sleep 32s
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("backoff took %v, expected immediate return on cancelled context", elapsed)
	}
}
