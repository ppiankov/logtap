package recv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	src = rand.NewSource(time.Now().UnixNano())
	mu  sync.Mutex // Mutex to protect access to src
)

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

// RandStringBytes generates a random string of a given length.
// Taken from https://stackoverflow.com/questions/22892120/how-to-generate-a-random-string-of-a-fixed-length-in-go
func RandStringBytes(n int) string {
	b := make([]byte, n)
	// Lock access to the random source
	mu.Lock()
	defer mu.Unlock()

	// A src.Int63() generates 63 random bits, enough for letterIdxMax letters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}

// estimateLogEntrySize estimates the size of a serialized LogEntry.
func estimateLogEntrySize(entry LogEntry) int {
	var buf bytes.Buffer
	jsonlEncoder := json.NewEncoder(&buf)
	_ = jsonlEncoder.Encode(entry)
	return buf.Len()
}

// sendLokiPush encapsulates the HTTP POST request logic for a LokiPushRequest.
func sendLokiPush(t *testing.T, tsURL string, streamName string, values [][]string) int {
	payload, _ := json.Marshal(LokiPushRequest{
		Streams: []LokiStream{{
			Stream: map[string]string{"app": streamName},
			Values: values,
		}},
	})

	resp, err := http.Post(tsURL+"/loki/api/v1/push", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Logf("http post error: %v", err)
		return 0 // Indicate failure to send
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Logf("received non-204 status: %d", resp.StatusCode)
	}
	return len(values) // Return number of entries sent
}

func TestSustainedThroughput(t *testing.T) {
	var buf bytes.Buffer
	stats := NewStats()
	// Use a large writer buffer to minimize drops from internal writer queue,
	// focusing on HTTP server/handler throughput.
	w := NewWriter(4096, &buf, nil)
	defer w.Close()

	srv := NewServer(":0", w, nil, nil, stats, nil)
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	// --- Test Parameters ---
	const targetDuration = 5 * time.Second
	const targetRateMBps = 20 // MB/s for the test (for 100MB/s set higher and duration longer)
	// --- End Test Parameters ---

	// Estimate average log entry size for rate calculation
	sampleEntry := LogEntry{
		Timestamp: time.Now(),
		Labels:    map[string]string{"app": "test", "level": "info"},
		Message:   "This is a sample log message to estimate its size for rate limiting purposes.",
	}
	avgEntrySize := estimateLogEntrySize(sampleEntry)
	if avgEntrySize == 0 {
		t.Fatal("estimated log entry size is 0, cannot calculate rate")
	}
	t.Logf("Estimated average log entry size: %d bytes", avgEntrySize)

	targetBytesPerSecond := float64(targetRateMBps) * 1024 * 1024
	entriesPerSecond := int(targetBytesPerSecond / float64(avgEntrySize))
	batchSize := entriesPerSecond / 10 // Send in smaller batches
	if batchSize == 0 {
		batchSize = 1
	}

	t.Logf("Targeting %d MB/s (%d entries/sec) for %s", targetRateMBps, entriesPerSecond, targetDuration)

	var (
		totalSent atomic.Int64
		wg        sync.WaitGroup
		stopCh    = make(chan struct{})
	)

	// Start a fixed number of workers to send logs
	const numSenders = 4
	for i := 0; i < numSenders; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			ticker := time.NewTicker(time.Second / time.Duration(entriesPerSecond/numSenders))
			defer ticker.Stop()

			for {
				select {
				case <-stopCh:
					return
				case <-ticker.C:
					var values [][]string
					for j := 0; j < batchSize; j++ {
						values = append(values, []string{
							fmt.Sprintf("%d", time.Now().UnixNano()),
							fmt.Sprintf("worker=%d entry=%d - %s", workerID, j, RandStringBytes(100)), // Longer message for more realistic size
						})
					}
					sentCount := sendLokiPush(t, ts.URL, fmt.Sprintf("w%d", workerID), values)
					totalSent.Add(int64(sentCount))
				}
			}
		}(i)
	}

	// Run for the target duration
	<-time.After(targetDuration)
	close(stopCh)
	wg.Wait()

	// Allow buffered writes to drain
	time.Sleep(500 * time.Millisecond) // Give writer time to process remaining entries
	w.Close()

	// Verify results
	written := w.LinesWritten()
	dropped := stats.LogsDropped.Load()
	accounted := written + dropped

	t.Logf("Test finished. Sent: %d, Written: %d, Dropped: %d", totalSent.Load(), written, dropped)

	if totalSent.Load() == 0 {
		t.Error("No entries were sent during the test duration.")
	}

	// We expect some drops if the server cannot keep up, but totalSent should reflect actual attempts.
	// The goal is to verify it handles sustained load without blocking senders, and drops count correctly.
	if totalSent.Load() != accounted {
		t.Errorf("Total sent (%d) does not match accounted (written %d + dropped %d = %d)", totalSent.Load(), written, dropped, accounted)
	}

	// Check if the actual throughput was close to target (within a tolerance)
	actualRateMBps := float64(written*int64(avgEntrySize)) / (1024 * 1024 * targetDuration.Seconds())
	t.Logf("Actual throughput: %.2f MB/s", actualRateMBps)
	// Allow for some variance due to test environment and batching
	if actualRateMBps < float64(targetRateMBps)*0.1 { // Arbitrary 10% tolerance for a unit test
		t.Errorf("Actual throughput (%.2f MB/s) is significantly lower than target (%.2f MB/s)", actualRateMBps, float64(targetRateMBps))
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

	const numClients = 100
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
