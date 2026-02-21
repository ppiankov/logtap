package buffers

import (
	"sync"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/logtypes"
)

func entry(msg string) logtypes.LogEntry {
	return logtypes.LogEntry{
		Timestamp: time.Now(),
		Labels:    map[string]string{"app": "test"},
		Message:   msg,
	}
}

func TestNewLogRing_DefaultCap(t *testing.T) {
	r := NewLogRing(0)
	if r.cap != defaultRingSize {
		t.Fatalf("expected cap %d, got %d", defaultRingSize, r.cap)
	}
}

func TestNewLogRing_NegativeCap(t *testing.T) {
	r := NewLogRing(-5)
	if r.cap != defaultRingSize {
		t.Fatalf("expected cap %d, got %d", defaultRingSize, r.cap)
	}
}

func TestNewLogRing_CustomCap(t *testing.T) {
	r := NewLogRing(42)
	if r.cap != 42 {
		t.Fatalf("expected cap 42, got %d", r.cap)
	}
}

func TestPushAndSnapshot(t *testing.T) {
	r := NewLogRing(5)

	r.Push(entry("a"))
	r.Push(entry("b"))
	r.Push(entry("c"))

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(snap))
	}
	if snap[0].Message != "a" || snap[1].Message != "b" || snap[2].Message != "c" {
		t.Fatalf("unexpected order: %v", snap)
	}
}

func TestSnapshot_Empty(t *testing.T) {
	r := NewLogRing(5)
	snap := r.Snapshot()
	if snap != nil {
		t.Fatalf("expected nil snapshot for empty ring, got %v", snap)
	}
}

func TestPush_Overwrites(t *testing.T) {
	r := NewLogRing(3)

	r.Push(entry("a"))
	r.Push(entry("b"))
	r.Push(entry("c"))
	r.Push(entry("d"))

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(snap))
	}
	if snap[0].Message != "b" || snap[1].Message != "c" || snap[2].Message != "d" {
		t.Fatalf("expected [b c d], got [%s %s %s]", snap[0].Message, snap[1].Message, snap[2].Message)
	}
}

func TestVersion(t *testing.T) {
	r := NewLogRing(5)

	if r.Version() != 0 {
		t.Fatalf("expected version 0, got %d", r.Version())
	}

	r.Push(entry("a"))
	if r.Version() != 1 {
		t.Fatalf("expected version 1, got %d", r.Version())
	}

	r.Push(entry("b"))
	r.Push(entry("c"))
	if r.Version() != 3 {
		t.Fatalf("expected version 3, got %d", r.Version())
	}
}

func TestSnapshotFiltered(t *testing.T) {
	r := NewLogRing(10)

	r.Push(logtypes.LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "web"}, Message: "hello"})
	r.Push(logtypes.LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "world"})
	r.Push(logtypes.LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "web"}, Message: "foo"})

	filtered := r.SnapshotFiltered(func(e logtypes.LogEntry) bool {
		return e.Labels["app"] == "web"
	})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered entries, got %d", len(filtered))
	}
	if filtered[0].Message != "hello" || filtered[1].Message != "foo" {
		t.Fatalf("unexpected filtered entries: %v", filtered)
	}
}

func TestSnapshotFiltered_Empty(t *testing.T) {
	r := NewLogRing(5)
	filtered := r.SnapshotFiltered(func(e logtypes.LogEntry) bool { return true })
	if filtered != nil {
		t.Fatalf("expected nil for empty ring, got %v", filtered)
	}
}

func TestSnapshotFiltered_NoMatch(t *testing.T) {
	r := NewLogRing(5)
	r.Push(entry("a"))

	filtered := r.SnapshotFiltered(func(e logtypes.LogEntry) bool { return false })
	if len(filtered) != 0 {
		t.Fatalf("expected 0 filtered entries, got %d", len(filtered))
	}
}

func TestConcurrentPushSnapshot(t *testing.T) {
	r := NewLogRing(100)
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			r.Push(entry("msg"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = r.Snapshot()
			_ = r.Version()
		}
	}()
	wg.Wait()

	snap := r.Snapshot()
	if len(snap) != 100 {
		t.Fatalf("expected 100 entries in ring, got %d", len(snap))
	}
	if r.Version() != 1000 {
		t.Fatalf("expected version 1000, got %d", r.Version())
	}
}
