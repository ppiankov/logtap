package recv

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestLokiPush(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	payload := `{"streams":[{"stream":{"app":"test"},"values":[["1234567890000000000","hello world"]]}]}`
	resp, err := http.Post(ts.URL+"/loki/api/v1/push", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	// give writer time to drain
	time.Sleep(50 * time.Millisecond)
	w.Close()

	if buf.Len() == 0 {
		t.Fatal("no output written")
	}

	var entry LogEntry
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("invalid JSONL: %v: %s", err, buf.String())
	}
	if entry.Message != "hello world" {
		t.Errorf("got msg %q, want %q", entry.Message, "hello world")
	}
	if entry.Labels["app"] != "test" {
		t.Errorf("got labels %v, want app=test", entry.Labels)
	}
}

func TestLokiPushInvalidJSON(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/loki/api/v1/push", "application/json", strings.NewReader("{invalid"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRawPush(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	payload := `{"ts":"2024-01-01T00:00:00Z","msg":"raw line 1"}
{"ts":"2024-01-01T00:00:01Z","msg":"raw line 2"}
`
	resp, err := http.Post(ts.URL+"/logtap/raw", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	time.Sleep(50 * time.Millisecond)
	w.Close()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
	}
}

func TestBackpressure(t *testing.T) {
	var buf bytes.Buffer
	// buffer size 1 to force drops
	w := NewWriter(1, &buf, nil)
	defer w.Close()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	srv := NewServer(":0", w, nil, m, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	// send many entries to trigger backpressure
	var values [][]string
	for i := 0; i < 100; i++ {
		values = append(values, []string{"1234567890000000000", "msg"})
	}
	payload, _ := json.Marshal(LokiPushRequest{
		Streams: []LokiStream{{
			Stream: map[string]string{"app": "bp"},
			Values: values,
		}},
	})

	resp, err := http.Post(ts.URL+"/loki/api/v1/push", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	// should still return 204 (never block sender)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGracefulShutdown(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(65536, &buf, nil)

	srv := NewServer(":0", w, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)

	// send data
	payload := `{"streams":[{"stream":{"app":"shutdown"},"values":[["1234567890000000000","drain me"]]}]}`
	resp, err := http.Post(ts.URL+"/loki/api/v1/push", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	// shut down server, then close writer to drain
	ts.Close()
	time.Sleep(50 * time.Millisecond)
	w.Close()

	if buf.Len() == 0 {
		t.Error("expected buffered entry to be drained on shutdown")
	}
}

func TestVersionEndpoint(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, nil, nil)
	srv.SetVersion("0.3.0")
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Version string `json:"version"`
		API     int    `json:"api"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Version != "0.3.0" {
		t.Errorf("version = %q, want %q", result.Version, "0.3.0")
	}
	if result.API != APIVersion {
		t.Errorf("api = %d, want %d", result.API, APIVersion)
	}
}

func TestVersionEndpointDefault(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Version != "dev" {
		t.Errorf("version = %q, want %q", result.Version, "dev")
	}
}

func TestServer_OversizedRequest(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	// maxRequestBytes is 10MB (10 << 20). Build a payload that exceeds it.
	// Create a JSON body with a single stream containing a huge message.
	bigMsg := strings.Repeat("A", 11<<20) // 11MB of 'A'
	payload := `{"streams":[{"stream":{"app":"big"},"values":[["1234567890000000000","` + bigMsg + `"]]}]}`

	resp, err := http.Post(ts.URL+"/loki/api/v1/push", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	// MaxBytesReader should cause a 400 Bad Request when the body exceeds the limit
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for oversized request, got %d", resp.StatusCode)
	}

	// Also test oversized request on the raw push endpoint
	rawPayload := `{"ts":"2024-01-01T00:00:00Z","msg":"` + bigMsg + `"}`
	resp2, err := http.Post(ts.URL+"/logtap/raw", "application/json", strings.NewReader(rawPayload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()

	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for oversized raw request, got %d", resp2.StatusCode)
	}
}

func TestRedactionIntegration(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	redactor, err := NewRedactor([]string{"email", "credit_card"})
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer(":0", w, redactor, nil, nil, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	payload := `{"streams":[{"stream":{"app":"pii"},"values":[["1234567890000000000","user test@example.com paid 4111111111111111"]]}]}`
	resp, err := http.Post(ts.URL+"/loki/api/v1/push", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	time.Sleep(50 * time.Millisecond)
	w.Close()

	output := buf.String()
	if strings.Contains(output, "test@example.com") {
		t.Error("email was not redacted")
	}
	if strings.Contains(output, "4111111111111111") {
		t.Error("credit card was not redacted")
	}
	if !strings.Contains(output, "[REDACTED:email]") {
		t.Error("expected [REDACTED:email] marker")
	}
	if !strings.Contains(output, "[REDACTED:cc]") {
		t.Error("expected [REDACTED:cc] marker")
	}
}
