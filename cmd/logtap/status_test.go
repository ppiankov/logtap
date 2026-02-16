package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestFetchReceiverMetrics(t *testing.T) {
	origTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = origTransport }()

	t.Run("success", func(t *testing.T) {
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := strings.NewReader(strings.Join([]string{
				"logtap_logs_received_total 123",
				"logtap_disk_usage_bytes 456",
				"logtap_logs_dropped_total 7",
			}, "\n"))
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(body),
				Header:     make(http.Header),
			}, nil
		})

		metrics := fetchReceiverMetrics("example:9000")
		if metrics == nil {
			t.Fatal("expected metrics, got nil")
		}
		if metrics.logsReceived != "123" {
			t.Errorf("logsReceived = %q, want 123", metrics.logsReceived)
		}
		if metrics.diskUsage != "456" {
			t.Errorf("diskUsage = %q, want 456", metrics.diskUsage)
		}
		if metrics.dropped != "7" {
			t.Errorf("dropped = %q, want 7", metrics.dropped)
		}
	})

	t.Run("error", func(t *testing.T) {
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		})

		if metrics := fetchReceiverMetrics("example:9000"); metrics != nil {
			t.Fatalf("expected nil metrics, got %+v", metrics)
		}
	})
}
