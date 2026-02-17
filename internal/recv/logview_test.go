package recv

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLogRingPushUnderCapacity(t *testing.T) {
	r := NewLogRing(5)
	r.Push(LogEntry{Message: "a"})
	r.Push(LogEntry{Message: "b"})
	r.Push(LogEntry{Message: "c"})

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("len = %d, want 3", len(snap))
	}
	if snap[0].Message != "a" || snap[1].Message != "b" || snap[2].Message != "c" {
		t.Errorf("got %v, want [a b c]", msgs(snap))
	}
}

func TestLogRingWrapAround(t *testing.T) {
	r := NewLogRing(3)
	r.Push(LogEntry{Message: "a"})
	r.Push(LogEntry{Message: "b"})
	r.Push(LogEntry{Message: "c"})
	r.Push(LogEntry{Message: "d"}) // overwrites "a"
	r.Push(LogEntry{Message: "e"}) // overwrites "b"

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("len = %d, want 3", len(snap))
	}
	if snap[0].Message != "c" || snap[1].Message != "d" || snap[2].Message != "e" {
		t.Errorf("got %v, want [c d e]", msgs(snap))
	}
}

func TestLogRingEmpty(t *testing.T) {
	r := NewLogRing(10)
	snap := r.Snapshot()
	if snap != nil {
		t.Errorf("expected nil snapshot for empty ring, got %v", snap)
	}
}

func TestLogRingVersion(t *testing.T) {
	r := NewLogRing(10)
	if r.Version() != 0 {
		t.Errorf("initial version = %d, want 0", r.Version())
	}

	r.Push(LogEntry{Message: "a"})
	if r.Version() != 1 {
		t.Errorf("version = %d, want 1", r.Version())
	}

	r.Push(LogEntry{Message: "b"})
	r.Push(LogEntry{Message: "c"})
	if r.Version() != 3 {
		t.Errorf("version = %d, want 3", r.Version())
	}
}

func TestLogRingDefaultCapacity(t *testing.T) {
	r := NewLogRing(0)
	// should not panic on push
	r.Push(LogEntry{Message: "x"})
	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Errorf("len = %d, want 1", len(snap))
	}
}

func TestLogRingExactCapacity(t *testing.T) {
	r := NewLogRing(3)
	r.Push(LogEntry{Message: "a"})
	r.Push(LogEntry{Message: "b"})
	r.Push(LogEntry{Message: "c"})

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("len = %d, want 3", len(snap))
	}
	if snap[0].Message != "a" || snap[1].Message != "b" || snap[2].Message != "c" {
		t.Errorf("got %v, want [a b c]", msgs(snap))
	}
}

func TestLogRingPreservesFields(t *testing.T) {
	r := NewLogRing(10)
	ts := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	r.Push(LogEntry{
		Timestamp: ts,
		Labels:    map[string]string{"app": "api"},
		Message:   "hello",
	})

	snap := r.Snapshot()
	if snap[0].Timestamp != ts {
		t.Errorf("timestamp = %v, want %v", snap[0].Timestamp, ts)
	}
	if snap[0].Labels["app"] != "api" {
		t.Errorf("labels = %v, want app=api", snap[0].Labels)
	}
}

func TestLogRingConcurrent(t *testing.T) {
	r := NewLogRing(100)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				r.Push(LogEntry{Message: fmt.Sprintf("%d-%d", id, j)})
				r.Snapshot()
				r.Version()
			}
		}(i)
	}
	wg.Wait()

	if r.Version() != 10000 {
		t.Errorf("version = %d, want 10000", r.Version())
	}
}

func TestLogRingSnapshotFiltered(t *testing.T) {
	r := NewLogRing(10)
	r.Push(LogEntry{Labels: map[string]string{"app": "api"}, Message: "a"})
	r.Push(LogEntry{Labels: map[string]string{"app": "web"}, Message: "b"})
	r.Push(LogEntry{Labels: map[string]string{"app": "api"}, Message: "c"})
	r.Push(LogEntry{Labels: map[string]string{"app": "worker"}, Message: "d"})

	snap := r.SnapshotFiltered(func(e LogEntry) bool {
		return e.Labels["app"] == "api"
	})
	if len(snap) != 2 {
		t.Fatalf("len = %d, want 2", len(snap))
	}
	if snap[0].Message != "a" || snap[1].Message != "c" {
		t.Errorf("got %v, want [a c]", msgs(snap))
	}
}

func TestLogRingSnapshotFilteredEmpty(t *testing.T) {
	r := NewLogRing(5)
	r.Push(LogEntry{Labels: map[string]string{"app": "api"}, Message: "a"})

	snap := r.SnapshotFiltered(func(e LogEntry) bool {
		return e.Labels["app"] == "none"
	})
	if len(snap) != 0 {
		t.Errorf("len = %d, want 0", len(snap))
	}
}

func TestLogRingSnapshotFilteredAll(t *testing.T) {
	r := NewLogRing(5)
	r.Push(LogEntry{Labels: map[string]string{"app": "api"}, Message: "a"})
	r.Push(LogEntry{Labels: map[string]string{"app": "api"}, Message: "b"})

	snap := r.SnapshotFiltered(func(e LogEntry) bool {
		return true
	})
	if len(snap) != 2 {
		t.Fatalf("len = %d, want 2", len(snap))
	}
}

func TestLogRingSnapshotFilteredEmptyRing(t *testing.T) {
	r := NewLogRing(5)
	snap := r.SnapshotFiltered(func(e LogEntry) bool { return true })
	if snap != nil {
		t.Errorf("expected nil, got %v", snap)
	}
}

func msgs(entries []LogEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Message
	}
	return out
}
