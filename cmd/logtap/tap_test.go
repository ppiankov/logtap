package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ppiankov/logtap/internal/sidecar"
)

func newReceiverServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Skipf("httptest.NewServer failed: %v", r)
		}
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	u, err := url.Parse(server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("parse server URL: %v", err)
	}

	return server, u.Host
}

func TestDoubleResource(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"16Mi", "32Mi"},
		{"25m", "50m"},
		{"100m", "200m"},
		{"1Gi", "2Gi"},
		{"", ""},
		{"abc", "abc"},
		{"64", "128"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := doubleResource(tt.input)
			if got != tt.want {
				t.Errorf("doubleResource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCheckReceiver(t *testing.T) {
	t.Run("reachable", func(t *testing.T) {
		server, host := newReceiverServer(t)
		t.Cleanup(server.Close)

		if err := checkReceiver(host); err != nil {
			t.Fatalf("expected reachable receiver, got error: %v", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		server, host := newReceiverServer(t)
		server.Close()

		if err := checkReceiver(host); err == nil {
			t.Fatal("expected error for unreachable receiver")
		}
	})
}

func TestRunTap_Validation(t *testing.T) {
	tests := []struct {
		name    string
		opts    tapOpts
		wantErr string
	}{
		{
			name:    "no mode specified",
			opts:    tapOpts{target: "localhost:9000", forwarder: sidecar.ForwarderLogtap},
			wantErr: "specify one of",
		},
		{
			name:    "multiple modes",
			opts:    tapOpts{deployment: "foo", statefulset: "bar", target: "localhost:9000", forwarder: sidecar.ForwarderLogtap},
			wantErr: "specify only one of",
		},
		{
			name:    "all without force",
			opts:    tapOpts{all: true, target: "localhost:9000", forwarder: sidecar.ForwarderLogtap},
			wantErr: "requires --force",
		},
		{
			name:    "invalid forwarder",
			opts:    tapOpts{deployment: "foo", target: "localhost:9000", forwarder: "invalid"},
			wantErr: "must be",
		},
		{
			name:    "fluent-bit without image",
			opts:    tapOpts{deployment: "foo", target: "localhost:9000", forwarder: sidecar.ForwarderFluentBit, image: sidecar.DefaultImage},
			wantErr: "required when using",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runTap(tt.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !containsString(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestTapCmd_InvalidTarget(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{"newline injection", "host\n[OUTPUT]\n    Name null"},
		{"space injection", "host port"},
		{"semicolon", "host;rm -rf /"},
		{"path traversal", "host:3100/path"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTapCmd()
			cmd.SetArgs([]string{"--target", tt.target, "--deployment", "foo"})
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected error for invalid target")
			}
			if !containsString(err.Error(), "invalid --target") {
				t.Errorf("error %q should mention invalid target", err.Error())
			}
		})
	}
}

func TestCheckReceiver_MockTransport(t *testing.T) {
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()

	t.Run("reachable", func(t *testing.T) {
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := io.NopCloser(strings.NewReader("ok"))
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       body,
				Header:     make(http.Header),
			}, nil
		})

		if err := checkReceiver("example:9000"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		})

		if err := checkReceiver("example:9000"); err == nil {
			t.Fatal("expected error for unreachable receiver")
		}
	})
}
