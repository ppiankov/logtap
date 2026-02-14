package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		input   string
		wantMsg string
		wantTS  bool // whether timestamp should parse successfully
	}{
		{
			input:   "2024-01-15T10:30:00.123456789Z hello world",
			wantMsg: "hello world",
			wantTS:  true,
		},
		{
			input:   "2024-01-15T10:30:00Z single word",
			wantMsg: "single word",
			wantTS:  true,
		},
		{
			input:   "no-timestamp-here",
			wantMsg: "no-timestamp-here",
			wantTS:  false,
		},
		{
			input:   "bad-ts actual message",
			wantMsg: "bad-ts actual message",
			wantTS:  false,
		},
	}

	for _, tt := range tests {
		ts, msg := ParseLogLine(tt.input)
		if msg != tt.wantMsg {
			t.Errorf("ParseLogLine(%q) msg = %q, want %q", tt.input, msg, tt.wantMsg)
		}
		if tt.wantTS {
			if ts.Year() != 2024 {
				t.Errorf("ParseLogLine(%q) year = %d, want 2024", tt.input, ts.Year())
			}
		} else {
			// should fall back to time.Now()
			if time.Since(ts) > time.Second {
				t.Errorf("ParseLogLine(%q) fallback timestamp too old: %v", tt.input, ts)
			}
		}
	}
}

func TestFilterContainers(t *testing.T) {
	containers := []corev1.Container{
		{Name: "app"},
		{Name: "logtap-forwarder-lt-a3f9"},
		{Name: "sidecar-proxy"},
		{Name: "logtap-forwarder-lt-b2c1"},
	}

	got := FilterContainers(containers)
	if len(got) != 2 {
		t.Fatalf("FilterContainers = %v, want 2 containers", got)
	}
	if got[0] != "app" {
		t.Errorf("got[0] = %q, want %q", got[0], "app")
	}
	if got[1] != "sidecar-proxy" {
		t.Errorf("got[1] = %q, want %q", got[1], "sidecar-proxy")
	}
}

func TestFilterContainers_AllForwarders(t *testing.T) {
	containers := []corev1.Container{
		{Name: "logtap-forwarder-lt-a3f9"},
	}

	got := FilterContainers(containers)
	if len(got) != 0 {
		t.Errorf("FilterContainers = %v, want empty", got)
	}
}

func TestDiscoverContainers(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
				{Name: "logtap-forwarder-lt-a3f9"},
				{Name: "sidecar-proxy"},
			},
		},
	}
	cs := fake.NewSimpleClientset(pod) //nolint:staticcheck
	r := NewReaderFromClient(cs, "test-pod", "default")

	got, err := r.DiscoverContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d containers, want 2", len(got))
	}
	if got[0] != "app" || got[1] != "sidecar-proxy" {
		t.Errorf("got %v, want [app sidecar-proxy]", got)
	}
}

func TestDiscoverContainers_NotFound(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck
	r := NewReaderFromClient(cs, "no-such-pod", "default")

	_, err := r.DiscoverContainers(context.Background())
	if err == nil {
		t.Fatal("expected error for missing pod")
	}
}

func TestFollow(t *testing.T) {
	logContent := "2024-01-15T10:30:00.000000000Z line one\n2024-01-15T10:30:01.000000000Z line two\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(logContent))
	}))
	defer srv.Close()

	cs, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	r := NewReaderFromClient(cs, "test-pod", "default")
	out := make(chan LogLine, 10)

	err = r.Follow(context.Background(), "app", out)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d lines, want 2", len(out))
	}

	l := <-out
	if l.Line != "line one" {
		t.Errorf("line = %q, want %q", l.Line, "line one")
	}
	if l.Container != "app" {
		t.Errorf("container = %q, want %q", l.Container, "app")
	}
}

func TestFollow_StreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cs, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	r := NewReaderFromClient(cs, "test-pod", "default")
	out := make(chan LogLine, 10)

	err = r.Follow(context.Background(), "app", out)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestFollowWithRetry_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("2024-01-15T10:30:00.000000000Z line\n"))
	}))
	defer srv.Close()

	cs, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	r := NewReaderFromClient(cs, "test-pod", "default")
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan LogLine, 100)

	done := make(chan error, 1)
	go func() {
		done <- r.followWithRetry(ctx, "app", out)
	}()

	// wait for at least one line
	select {
	case <-out:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for line")
	}

	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("followWithRetry did not return after cancel")
	}
}

func TestFollowWithRetry_ErrorRetry(t *testing.T) {
	// server returns 500, triggering Follow error and the error print path
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cs, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	r := NewReaderFromClient(cs, "test-pod", "default")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	out := make(chan LogLine, 10)
	retErr := r.followWithRetry(ctx, "app", out)
	if retErr != context.DeadlineExceeded {
		t.Errorf("err = %v, want context.DeadlineExceeded", retErr)
	}
}

func TestFollowAll_NoContainers(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "logtap-forwarder-lt-a3f9"},
			},
		},
	}
	cs := fake.NewSimpleClientset(pod) //nolint:staticcheck
	r := NewReaderFromClient(cs, "test-pod", "default")

	out := make(chan LogLine, 10)
	err := r.FollowAll(context.Background(), out)
	if err == nil {
		t.Fatal("expected error for no sibling containers")
	}
	if !strings.Contains(err.Error(), "no sibling containers") {
		t.Errorf("error = %q, want 'no sibling containers'", err.Error())
	}
}

func TestFollowAll(t *testing.T) {
	// pod with one real container — FollowAll discovers it, spawns followWithRetry
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
			},
		},
	}
	cs := fake.NewSimpleClientset(pod) //nolint:staticcheck
	r := NewReaderFromClient(cs, "test-pod", "default")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	out := make(chan LogLine, 10)
	err := r.FollowAll(ctx, out)
	// FollowAll blocks until ctx.Done(), returns nil
	if err != nil {
		t.Errorf("FollowAll returned error: %v", err)
	}
}
