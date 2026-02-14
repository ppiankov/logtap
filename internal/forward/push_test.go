package forward

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPush_Success(t *testing.T) {
	var received lokiPushRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, pushPath) {
			t.Errorf("path = %s, want %s", r.URL.Path, pushPath)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	p := NewPusher(addr)

	labels := map[string]string{"namespace": "default", "pod": "api-gw-abc"}
	lines := []TimestampedLine{
		{Timestamp: time.Unix(0, 1000000000), Line: "hello world"},
		{Timestamp: time.Unix(0, 2000000000), Line: "second line"},
	}

	err := p.Push(context.Background(), labels, lines)
	if err != nil {
		t.Fatal(err)
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
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	p := NewPusher(addr)

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
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	ts.Close() // close immediately to trigger connection refused

	addr := strings.TrimPrefix(ts.URL, "http://")
	p := NewPusher(addr)

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
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	p := NewPusher(addr)

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
