package buffers

import (
	"sync"

	"github.com/ppiankov/logtap/internal/logtypes" // Explicitly import LogEntry
)

const defaultRingSize = 10_000

// LogRing is a fixed-size circular buffer of log entries for TUI display.
// All methods are safe for concurrent use.
type LogRing struct {
	mu      sync.Mutex
	buf     []logtypes.LogEntry // Use logtypes.LogEntry
	cap     int
	head    int // next write position
	count   int // entries in buffer (≤ cap)
	version int // monotonic counter for change detection
}

// NewLogRing creates a ring buffer with the given capacity.
// If cap ≤ 0, defaultRingSize is used.
func NewLogRing(cap int) *LogRing {
	if cap <= 0 {
		cap = defaultRingSize
	}
	return &LogRing{
		buf: make([]logtypes.LogEntry, cap), // Use logtypes.LogEntry
		cap: cap,
	}
}

// Push adds an entry to the ring. If full, the oldest entry is overwritten.
// Never blocks.
func (r *LogRing) Push(entry logtypes.LogEntry) { // Use logtypes.LogEntry
	r.mu.Lock()
	r.buf[r.head] = entry
	r.head = (r.head + 1) % r.cap
	if r.count < r.cap {
		r.count++
	}
	r.version++
	r.mu.Unlock()
}

// Snapshot returns a chronological copy of all entries in the ring.
func (r *LogRing) Snapshot() []logtypes.LogEntry { // Use logtypes.LogEntry
	r.mu.Lock()
	n := r.count
	if n == 0 {
		r.mu.Unlock()
		return nil
	}

	out := make([]logtypes.LogEntry, n) // Use logtypes.LogEntry
	start := (r.head - n + r.cap) % r.cap
	for i := 0; i < n; i++ {
		out[i] = r.buf[(start+i)%r.cap]
	}
	r.mu.Unlock()
	return out
}

// SnapshotFiltered returns a chronological copy of entries matching the predicate.
func (r *LogRing) SnapshotFiltered(fn func(logtypes.LogEntry) bool) []logtypes.LogEntry { // Use logtypes.LogEntry
	r.mu.Lock()
	n := r.count
	if n == 0 {
		r.mu.Unlock()
		return nil
	}

	var out []logtypes.LogEntry // Use logtypes.LogEntry
	start := (r.head - n + r.cap) % r.cap
	for i := 0; i < n; i++ {
		entry := r.buf[(start+i)%r.cap]
		if fn(entry) {
			out = append(out, entry)
		}
	}
	r.mu.Unlock()
	return out
}

// Version returns a monotonic counter that increments on every Push.
func (r *LogRing) Version() int {
	r.mu.Lock()
	v := r.version
	r.mu.Unlock()
	return v
}
