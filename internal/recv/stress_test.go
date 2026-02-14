package recv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSustainedThroughput(t *testing.T) {
	var buf bytes.Buffer
	stats := NewStats()
	w := NewWriter(1024, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, stats, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	const numWorkers = 10
	const entriesPerWorker = 1000

	var wg sync.WaitGroup
	var badStatus atomic.Int64

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			// each worker sends one big request with all its entries
			var values [][]string
			for j := 0; j < entriesPerWorker; j++ {
				values = append(values, []string{
					fmt.Sprintf("%d", time.Now().UnixNano()),
					fmt.Sprintf("worker=%d entry=%d", workerID, j),
				})
			}
			payload, _ := json.Marshal(LokiPushRequest{
				Streams: []LokiStream{{
					Stream: map[string]string{"app": fmt.Sprintf("w%d", workerID)},
					Values: values,
				}},
			})

			resp, err := http.Post(ts.URL+"/loki/api/v1/push", "application/json", bytes.NewReader(payload))
			if err != nil {
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				badStatus.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// allow drain
	time.Sleep(100 * time.Millisecond)
	w.Close()

	if badStatus.Load() != 0 {
		t.Errorf("got %d non-204 responses", badStatus.Load())
	}

	total := int64(numWorkers * entriesPerWorker)
	accounted := w.LinesWritten() + stats.LogsDropped.Load()
	if accounted != total {
		t.Errorf("written(%d) + dropped(%d) = %d, want %d", w.LinesWritten(), stats.LogsDropped.Load(), accounted, total)
	}
}

func TestConcurrentConnections(t *testing.T) {
	var buf bytes.Buffer
	stats := NewStats()
	w := NewWriter(256, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, stats, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	const numClients = 50
	const requestsPerClient = 5
	const entriesPerRequest = 20

	var wg sync.WaitGroup
	var badStatus atomic.Int64

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			for r := 0; r < requestsPerClient; r++ {
				var values [][]string
				for e := 0; e < entriesPerRequest; e++ {
					values = append(values, []string{
						fmt.Sprintf("%d", time.Now().UnixNano()),
						fmt.Sprintf("c=%d r=%d e=%d", clientID, r, e),
					})
				}
				payload, _ := json.Marshal(LokiPushRequest{
					Streams: []LokiStream{{
						Stream: map[string]string{"app": fmt.Sprintf("c%d", clientID)},
						Values: values,
					}},
				})

				resp, err := http.Post(ts.URL+"/loki/api/v1/push", "application/json", bytes.NewReader(payload))
				if err != nil {
					continue
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					badStatus.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	// allow drain
	time.Sleep(100 * time.Millisecond)
	w.Close()

	if badStatus.Load() != 0 {
		t.Errorf("got %d non-204 responses", badStatus.Load())
	}

	total := int64(numClients * requestsPerClient * entriesPerRequest)
	accounted := w.LinesWritten() + stats.LogsDropped.Load()
	if accounted != total {
		t.Errorf("written(%d) + dropped(%d) = %d, want %d", w.LinesWritten(), stats.LogsDropped.Load(), accounted, total)
	}

	// active connections should return to 0
	if stats.ActiveConns.Load() != 0 {
		t.Errorf("ActiveConns = %d, want 0", stats.ActiveConns.Load())
	}
}

// slowWriter wraps an io.Writer and adds a delay per write to force channel saturation.
type slowWriter struct {
	dst   io.Writer
	delay time.Duration
}

func (s *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(s.delay)
	return s.dst.Write(p)
}

func TestDropCounterAccuracy(t *testing.T) {
	var buf bytes.Buffer
	slow := &slowWriter{dst: &buf, delay: 10 * time.Millisecond}
	stats := NewStats()
	w := NewWriter(1, slow, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, stats, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	const totalEntries = 100
	var values [][]string
	for i := 0; i < totalEntries; i++ {
		values = append(values, []string{
			fmt.Sprintf("%d", time.Now().UnixNano()),
			fmt.Sprintf("msg %d", i),
		})
	}
	payload, _ := json.Marshal(LokiPushRequest{
		Streams: []LokiStream{{
			Stream: map[string]string{"app": "drop"},
			Values: values,
		}},
	})

	resp, err := http.Post(ts.URL+"/loki/api/v1/push", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	// wait for slow writer to finish draining
	time.Sleep(200 * time.Millisecond)
	w.Close()

	written := w.LinesWritten()
	dropped := stats.LogsDropped.Load()

	if written+dropped != totalEntries {
		t.Errorf("written(%d) + dropped(%d) = %d, want %d", written, dropped, written+dropped, totalEntries)
	}
	if dropped == 0 {
		t.Error("expected some drops with slow writer and buffer=1")
	}
}

func TestGracefulShutdownDrain(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(4096, &buf, nil)

	const totalEntries = 500
	for i := 0; i < totalEntries; i++ {
		entry := LogEntry{
			Timestamp: time.Now(),
			Labels:    map[string]string{"app": "drain"},
			Message:   fmt.Sprintf("entry %d", i),
		}
		if !w.Send(entry) {
			t.Fatalf("send %d failed unexpectedly (buffer=4096)", i)
		}
	}

	// close immediately — should drain all buffered entries
	w.Close()

	if w.LinesWritten() != totalEntries {
		t.Errorf("LinesWritten = %d, want %d", w.LinesWritten(), totalEntries)
	}
}

func TestWriterCloseIdempotent(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(16, &buf, nil)

	w.Send(LogEntry{Timestamp: time.Now(), Message: "test"})

	// first close
	w.Close()

	// second close — should not panic
	w.Close()

	// send after close — should not panic regardless of return value
	w.Send(LogEntry{Timestamp: time.Now(), Message: "after close"})
}
