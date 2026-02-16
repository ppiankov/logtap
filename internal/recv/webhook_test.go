package recv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestDispatcher_Fire(t *testing.T) {
	var mu sync.Mutex
	var received []WebhookEvent

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var evt WebhookEvent
		if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
			t.Errorf("decode webhook: %v", err)
			return
		}
		mu.Lock()
		received = append(received, evt)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewWebhookDispatcher([]string{srv.URL}, nil)
	d.Fire(WebhookEvent{
		Event:     "start",
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Dir:       "/tmp/capture",
	})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Event != "start" {
		t.Errorf("event = %q, want %q", received[0].Event, "start")
	}
	if received[0].Dir != "/tmp/capture" {
		t.Errorf("dir = %q, want %q", received[0].Dir, "/tmp/capture")
	}
}

func TestDispatcher_EventFilter(t *testing.T) {
	var mu sync.Mutex
	var received []WebhookEvent

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var evt WebhookEvent
		if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
			return
		}
		mu.Lock()
		received = append(received, evt)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewWebhookDispatcher([]string{srv.URL}, []string{"start", "stop"})

	d.Fire(WebhookEvent{Event: "rotation", Detail: "size"})
	d.Fire(WebhookEvent{Event: "start", Dir: "/capture"})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event (rotation filtered), got %d", len(received))
	}
	if received[0].Event != "start" {
		t.Errorf("event = %q, want %q", received[0].Event, "start")
	}
}

func TestDispatcher_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer srv.Close()

	d := NewWebhookDispatcher([]string{srv.URL}, nil)

	start := time.Now()
	d.Fire(WebhookEvent{Event: "start"})
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("Fire blocked for %v, expected non-blocking", elapsed)
	}
}

func TestDispatcher_NilDispatcher(t *testing.T) {
	var d *WebhookDispatcher
	// Should not panic
	d.Fire(WebhookEvent{Event: "start"})
}

func TestDispatcher_MultipleURLs(t *testing.T) {
	var mu sync.Mutex
	counts := make(map[string]int)

	handler := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			counts[name]++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		})
	}

	srv1 := httptest.NewServer(handler("srv1"))
	defer srv1.Close()
	srv2 := httptest.NewServer(handler("srv2"))
	defer srv2.Close()

	d := NewWebhookDispatcher([]string{srv1.URL, srv2.URL}, nil)
	d.Fire(WebhookEvent{Event: "stop"})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if counts["srv1"] != 1 {
		t.Errorf("srv1 received %d events, want 1", counts["srv1"])
	}
	if counts["srv2"] != 1 {
		t.Errorf("srv2 received %d events, want 1", counts["srv2"])
	}
}

func TestNewDispatcher_NoURLs(t *testing.T) {
	d := NewWebhookDispatcher(nil, nil)
	if d != nil {
		t.Error("expected nil dispatcher for empty URLs")
	}
	// Should not panic
	d.Fire(WebhookEvent{Event: "start"})
}

func TestDispatcher_WithStats(t *testing.T) {
	var mu sync.Mutex
	var received WebhookEvent

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewWebhookDispatcher([]string{srv.URL}, nil)
	d.Fire(WebhookEvent{
		Event: "stop",
		Dir:   "/capture",
		Stats: &WebhookStats{
			LinesWritten: 1000,
			BytesWritten: 50000,
			DiskUsage:    40000,
			DiskCap:      100000,
		},
	})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if received.Stats == nil {
		t.Fatal("expected stats in payload")
	}
	if received.Stats.LinesWritten != 1000 {
		t.Errorf("lines = %d, want 1000", received.Stats.LinesWritten)
	}
}
