package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/forward"
)

type fakeReader struct {
	lines []forward.LogLine
	err   error
}

func (r fakeReader) FollowAll(ctx context.Context, out chan<- forward.LogLine) error {
	for _, line := range r.lines {
		select {
		case out <- line:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.err
}

type pushCall struct {
	labels map[string]string
	lines  []forward.TimestampedLine
}

type scriptedPusher struct {
	calls      chan<- pushCall
	errOnFirst error
	count      uint32
}

func (p *scriptedPusher) Push(ctx context.Context, labels map[string]string, lines []forward.TimestampedLine) error {
	labelsCopy := make(map[string]string, len(labels))
	for k, v := range labels {
		labelsCopy[k] = v
	}
	linesCopy := make([]forward.TimestampedLine, len(lines))
	copy(linesCopy, lines)

	p.calls <- pushCall{labels: labelsCopy, lines: linesCopy}

	p.count++
	if p.count == 1 && p.errOnFirst != nil {
		return p.errOnFirst
	}
	return nil
}

func waitForPush(t *testing.T, ch <-chan pushCall) pushCall {
	t.Helper()
	select {
	case call := <-ch:
		return call
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for push")
		return pushCall{}
	}
}

func waitForResponse(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			return resp
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s: %v", url, lastErr)
	return nil
}

type inMemoryListener struct {
	conns  chan net.Conn
	closed chan struct{}
}

func newInMemoryListener() *inMemoryListener {
	return &inMemoryListener{
		conns:  make(chan net.Conn, 1),
		closed: make(chan struct{}),
	}
}

func (l *inMemoryListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *inMemoryListener) Close() error {
	select {
	case <-l.closed:
		return nil
	default:
		close(l.closed)
		return nil
	}
}

func (l *inMemoryListener) Addr() net.Addr {
	return dummyAddr("inmem")
}

func (l *inMemoryListener) DialContext(ctx context.Context) (net.Conn, error) {
	server, client := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-ctx.Done():
		_ = server.Close()
		_ = client.Close()
		return nil, ctx.Err()
	case <-l.closed:
		_ = server.Close()
		_ = client.Close()
		return nil, net.ErrClosed
	}
}

type dummyAddr string

func (d dummyAddr) Network() string { return string(d) }
func (d dummyAddr) String() string  { return string(d) }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestStartHealthServerOK(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ln := newInMemoryListener()
	addr, err := startHealthServerWithListener(ctx, ln, io.Discard)
	if err != nil {
		t.Fatalf("startHealthServerWithListener: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return ln.DialContext(ctx)
			},
		},
		Timeout: 200 * time.Millisecond,
	}

	resp := waitForResponse(t, client, "http://"+addr+"/healthz")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != `{"status":"ok"}` {
		t.Fatalf("body = %s, want %s", string(body), `{"status":"ok"}`)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
}

func TestValidateConfigMissing(t *testing.T) {
	base := Config{
		Target:    "target",
		Session:   "session",
		PodName:   "pod",
		Namespace: "namespace",
	}

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "missing target", cfg: Config{Session: base.Session, PodName: base.PodName, Namespace: base.Namespace}, want: envTarget},
		{name: "missing session", cfg: Config{Target: base.Target, PodName: base.PodName, Namespace: base.Namespace}, want: envSession},
		{name: "missing pod", cfg: Config{Target: base.Target, Session: base.Session, Namespace: base.Namespace}, want: envPodName},
		{name: "missing namespace", cfg: Config{Target: base.Target, Session: base.Session, PodName: base.PodName}, want: envNamespace},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %s", err, tt.want)
			}
		})
	}

	if err := validateConfig(base); err != nil {
		t.Fatalf("validateConfig: %v", err)
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	env := map[string]string{
		envTarget:    "target",
		envSession:   "session",
		envPodName:   "pod",
		envNamespace: "namespace",
	}

	cfg, err := loadConfigFromEnv(func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("loadConfigFromEnv: %v", err)
	}
	if cfg.HealthAddr != defaultHealthAddr {
		t.Fatalf("HealthAddr = %q, want %q", cfg.HealthAddr, defaultHealthAddr)
	}
	if cfg.Target != env[envTarget] || cfg.Session != env[envSession] || cfg.PodName != env[envPodName] || cfg.Namespace != env[envNamespace] {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestLoadConfigFromEnvMissing(t *testing.T) {
	_, err := loadConfigFromEnv(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), envTarget) {
		t.Fatalf("err = %v, want missing %s", err, envTarget)
	}
}

func TestRunInvalidConfig(t *testing.T) {
	err := run(context.Background(), Config{}, Dependencies{})
	if err == nil || !strings.Contains(err.Error(), envTarget) {
		t.Fatalf("err = %v, want missing %s", err, envTarget)
	}
}

func TestRunReaderError(t *testing.T) {
	cfg := Config{
		Target:    "target",
		Session:   "session",
		PodName:   "pod",
		Namespace: "namespace",
	}

	deps := Dependencies{
		NewReader: func(string, string) (logReader, error) {
			return nil, errors.New("boom")
		},
		LogWriter: io.Discard,
	}

	err := run(context.Background(), cfg, deps)
	if err == nil || !strings.Contains(err.Error(), "init reader") {
		t.Fatalf("err = %v, want init reader error", err)
	}
}

func TestRunFlushesAndLogsBufferExceeded(t *testing.T) {
	cfg := Config{
		Target:    "receiver",
		Session:   "session",
		PodName:   "pod",
		Namespace: "namespace",
	}

	now := time.Unix(1700000000, 0).UTC()
	reader := fakeReader{
		lines: []forward.LogLine{
			{Timestamp: now, Container: "app", Line: "first"},
			{Timestamp: now.Add(1 * time.Second), Container: "app", Line: "second"},
			{Timestamp: now.Add(2 * time.Second), Container: "sidecar", Line: "third"},
		},
	}

	pushCh := make(chan pushCall, 2)
	pusher := &scriptedPusher{
		calls:      pushCh,
		errOnFirst: forward.ErrBufferExceeded,
	}

	var logs bytes.Buffer
	deps := Dependencies{
		NewReader: func(string, string) (logReader, error) {
			return reader, nil
		},
		NewPusher: func(target string) logPusher {
			if target != cfg.Target {
				t.Fatalf("target = %q, want %q", target, cfg.Target)
			}
			return pusher
		},
		LogWriter: &logs,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, cfg, deps)
	}()

	first := waitForPush(t, pushCh)
	if first.labels["container"] != "app" {
		t.Fatalf("first container = %q, want app", first.labels["container"])
	}
	if first.labels["namespace"] != cfg.Namespace || first.labels["pod"] != cfg.PodName || first.labels["session"] != cfg.Session {
		t.Fatalf("first labels missing base labels: %#v", first.labels)
	}
	if len(first.lines) != 2 || first.lines[0].Line != "first" || first.lines[1].Line != "second" {
		t.Fatalf("first lines = %#v", first.lines)
	}

	cancel()

	second := waitForPush(t, pushCh)
	if second.labels["container"] != "sidecar" {
		t.Fatalf("second container = %q, want sidecar", second.labels["container"])
	}
	if len(second.lines) != 1 || second.lines[0].Line != "third" {
		t.Fatalf("second lines = %#v", second.lines)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for run to finish")
	}

	if !strings.Contains(logs.String(), "batch too large, dropping 2 lines") {
		t.Fatalf("expected buffer exceeded log, got %q", logs.String())
	}
}

func TestPusherSendsToReceiver(t *testing.T) {
	type requestCapture struct {
		method      string
		path        string
		contentType string
		body        []byte
	}

	captureCh := make(chan requestCapture, 1)
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			captureCh <- requestCapture{
				method:      r.Method,
				path:        r.URL.Path,
				contentType: r.Header.Get("Content-Type"),
				body:        body,
			}
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout: 200 * time.Millisecond,
	}

	pusher := forward.NewPusherWithClient("receiver:3100", client)

	err := pusher.Push(context.Background(), map[string]string{"job": "test"}, []forward.TimestampedLine{
		{Timestamp: time.Unix(0, 1), Line: "hello"},
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	var capture requestCapture
	select {
	case capture = <-captureCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for receiver request")
	}

	if capture.method != http.MethodPost {
		t.Fatalf("method = %s, want POST", capture.method)
	}
	if capture.path != "/loki/api/v1/push" {
		t.Fatalf("path = %s, want /loki/api/v1/push", capture.path)
	}
	if capture.contentType != "application/json" {
		t.Fatalf("content-type = %q, want application/json", capture.contentType)
	}

	var payload struct {
		Streams []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(capture.body, &payload); err != nil {
		t.Fatalf("invalid JSON payload: %v", err)
	}
	if len(payload.Streams) != 1 || payload.Streams[0].Stream["job"] != "test" {
		t.Fatalf("unexpected streams: %#v", payload.Streams)
	}
	if len(payload.Streams[0].Values) != 1 || len(payload.Streams[0].Values[0]) != 2 || payload.Streams[0].Values[0][1] != "hello" {
		t.Fatalf("unexpected values: %#v", payload.Streams[0].Values)
	}
}
