package recv

import "sync"

const defaultRingSize = 10_000

// LogRing is a fixed-size circular buffer of log entries for TUI display.
// All methods are safe for concurrent use.
type LogRing struct {
	mu      sync.Mutex
	buf     []LogEntry
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
		buf: make([]LogEntry, cap),
		cap: cap,
	}
}

// Push adds an entry to the ring. If full, the oldest entry is overwritten.
// Never blocks.
func (r *LogRing) Push(entry LogEntry) {
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
func (r *LogRing) Snapshot() []LogEntry {
	r.mu.Lock()
	n := r.count
	if n == 0 {
		r.mu.Unlock()
		return nil
	}

	out := make([]LogEntry, n)
	start := (r.head - n + r.cap) % r.cap
	for i := 0; i < n; i++ {
		out[i] = r.buf[(start+i)%r.cap]
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
