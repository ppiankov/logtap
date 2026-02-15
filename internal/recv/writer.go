package recv

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// LogEntry represents a single parsed log line.
type LogEntry struct {
	Timestamp time.Time         `json:"ts"`
	Labels    map[string]string `json:"labels,omitempty"`
	Message   string            `json:"msg"`
}

// Writer drains LogEntry from a bounded channel and writes JSONL to a destination.
type Writer struct {
	ch     chan LogEntry
	dst    io.Writer
	track  func(time.Time, map[string]string) // called per line for index tracking
	done   chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool

	bytesWritten atomic.Int64
	linesWritten atomic.Int64

	queueGauge func(float64) // optional callback to report queue length
}

// NewWriter creates a Writer with the given buffer size.
// dst receives JSONL output; track is called per line for metadata tracking (may be nil).
func NewWriter(bufSize int, dst io.Writer, track func(time.Time, map[string]string)) *Writer {
	w := &Writer{
		ch:    make(chan LogEntry, bufSize),
		dst:   dst,
		track: track,
		done:  make(chan struct{}),
	}
	w.wg.Add(1)
	go w.drain()
	return w
}

// SetQueueGauge sets a callback to report queue length changes.
func (w *Writer) SetQueueGauge(fn func(float64)) {
	w.queueGauge = fn
}

// Send attempts a non-blocking send of entry to the writer channel.
// Returns false if the channel is full (caller should count as dropped).
func (w *Writer) Send(entry LogEntry) bool {
	select {
	case w.ch <- entry:
		w.reportQueue()
		return true
	default:
		return false
	}
}

func (w *Writer) reportQueue() {
	if w.queueGauge != nil {
		w.queueGauge(float64(len(w.ch)))
	}
}

// Close signals the writer to stop, drains remaining entries, and waits.
func (w *Writer) Close() {
	if w.closed.CompareAndSwap(false, true) {
		close(w.done)
		w.wg.Wait()
	}
}

// BytesWritten returns total bytes written.
func (w *Writer) BytesWritten() int64 { return w.bytesWritten.Load() }

// LinesWritten returns total lines written.
func (w *Writer) LinesWritten() int64 { return w.linesWritten.Load() }

// Healthy returns true if the writer channel has capacity (not in backpressure).
func (w *Writer) Healthy() bool { return len(w.ch) < cap(w.ch) }

func (w *Writer) drain() {
	defer w.wg.Done()
	for {
		select {
		case entry := <-w.ch:
			w.writeLine(entry)
			w.reportQueue()
		case <-w.done:
			// drain remaining
			for {
				select {
				case entry := <-w.ch:
					w.writeLine(entry)
					w.reportQueue()
				default:
					return
				}
			}
		}
	}
}

func (w *Writer) writeLine(entry LogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line := fmt.Sprintf("%s\n", data)
	n, _ := io.WriteString(w.dst, line)
	w.bytesWritten.Add(int64(n))
	w.linesWritten.Add(1)
	if w.track != nil {
		w.track(entry.Timestamp, entry.Labels)
	}
}
