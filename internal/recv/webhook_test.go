package recv

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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

	d, err := NewWebhookDispatcher([]string{srv.URL}, nil, "")
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
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

	d, err := NewWebhookDispatcher([]string{srv.URL}, []string{"start", "stop"}, "")
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

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

	d, err := NewWebhookDispatcher([]string{srv.URL}, nil, "")
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

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

	d, err := NewWebhookDispatcher([]string{srv1.URL, srv2.URL}, nil, "")
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
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
	d, err := NewWebhookDispatcher(nil, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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

	d, err := NewWebhookDispatcher([]string{srv.URL}, nil, "")
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
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

func TestWebhook_NoAuth(t *testing.T) {
	var mu sync.Mutex
	var headers http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d, err := NewWebhookDispatcher([]string{srv.URL}, nil, "")
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	d.Fire(WebhookEvent{Event: "start"})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if headers.Get("Authorization") != "" {
		t.Errorf("unexpected Authorization header: %q", headers.Get("Authorization"))
	}
	if headers.Get("X-Logtap-Signature") != "" {
		t.Errorf("unexpected X-Logtap-Signature header: %q", headers.Get("X-Logtap-Signature"))
	}
}

func TestWebhook_BearerAuth(t *testing.T) {
	var mu sync.Mutex
	var headers http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d, err := NewWebhookDispatcher([]string{srv.URL}, nil, "bearer:my-secret-token")
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	d.Fire(WebhookEvent{Event: "start"})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	want := "Bearer my-secret-token"
	if got := headers.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	if headers.Get("X-Logtap-Signature") != "" {
		t.Errorf("unexpected X-Logtap-Signature header with bearer auth")
	}
}

func TestWebhook_HMACAuth(t *testing.T) {
	var mu sync.Mutex
	var headers http.Header
	var body []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = r.Header.Clone()
		body, _ = io.ReadAll(r.Body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	secret := "webhook-secret"
	d, err := NewWebhookDispatcher([]string{srv.URL}, nil, "hmac-sha256:"+secret)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	d.Fire(WebhookEvent{Event: "start"})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	sig := headers.Get("X-Logtap-Signature")
	if sig == "" {
		t.Fatal("missing X-Logtap-Signature header")
	}

	// Verify the HMAC is correct
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig != want {
		t.Errorf("X-Logtap-Signature = %q, want %q", sig, want)
	}

	if headers.Get("Authorization") != "" {
		t.Errorf("unexpected Authorization header with HMAC auth")
	}
}

func TestWebhook_InvalidAuthFormat(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{"no colon", "bearertoken"},
		{"trailing colon only", "bearer:"},
		{"unsupported mode", "basic:user:pass"},
		{"empty mode", ":value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWebhookDispatcher([]string{"http://example.com"}, nil, tt.spec)
			if err == nil {
				t.Errorf("expected error for auth spec %q", tt.spec)
			}
		})
	}
}

func TestParseWebhookAuth(t *testing.T) {
	tests := []struct {
		spec      string
		wantMode  string
		wantValue string
		wantErr   bool
	}{
		{"", "", "", false},
		{"bearer:tok123", "bearer", "tok123", false},
		{"hmac-sha256:secret", "hmac-sha256", "secret", false},
		{"bearer:", "", "", true},
		{"nocolon", "", "", true},
		{"basic:creds", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			mode, value, err := ParseWebhookAuth(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if mode != tt.wantMode {
				t.Errorf("mode = %q, want %q", mode, tt.wantMode)
			}
			if value != tt.wantValue {
				t.Errorf("value = %q, want %q", value, tt.wantValue)
			}
		})
	}
}
