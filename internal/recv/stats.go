package recv

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Stats collects pipeline counters for TUI display.
// All methods are safe for concurrent use.
type Stats struct {
	LogsReceived atomic.Int64
	LogsDropped  atomic.Int64
	ActiveConns  atomic.Int64

	mu      sync.Mutex
	talkers map[string]int64
}

// NewStats creates a Stats collector.
func NewStats() *Stats {
	return &Stats{
		talkers: make(map[string]int64),
	}
}

// RecordEntry increments received counter and tracks the talker.
// The talker name is the "app" label value, falling back to the first label value.
func (s *Stats) RecordEntry(labels map[string]string) {
	s.LogsReceived.Add(1)

	name := labels["app"]
	if name == "" {
		for _, v := range labels {
			name = v
			break
		}
	}
	if name == "" {
		return
	}

	s.mu.Lock()
	s.talkers[name]++
	s.mu.Unlock()
}

// RecordDrop increments the dropped counter.
func (s *Stats) RecordDrop() {
	s.LogsDropped.Add(1)
}

// Talker is a name and its cumulative entry count.
type Talker struct {
	Name  string
	Count int64
}

// Snapshot is a point-in-time copy of pipeline stats.
type Snapshot struct {
	LogsReceived int64
	LogsDropped  int64
	ActiveConns  int64
	DiskUsage    int64
	DiskCap      int64
	BytesWritten int64
	Talkers      []Talker
}

// Snapshot returns a point-in-time copy of all stats.
func (s *Stats) Snapshot(diskUsage, diskCap, bytesWritten int64) Snapshot {
	snap := Snapshot{
		LogsReceived: s.LogsReceived.Load(),
		LogsDropped:  s.LogsDropped.Load(),
		ActiveConns:  s.ActiveConns.Load(),
		DiskUsage:    diskUsage,
		DiskCap:      diskCap,
		BytesWritten: bytesWritten,
	}

	s.mu.Lock()
	snap.Talkers = make([]Talker, 0, len(s.talkers))
	for name, count := range s.talkers {
		snap.Talkers = append(snap.Talkers, Talker{Name: name, Count: count})
	}
	s.mu.Unlock()

	sort.Slice(snap.Talkers, func(i, j int) bool {
		return snap.Talkers[i].Count > snap.Talkers[j].Count
	})

	return snap
}
