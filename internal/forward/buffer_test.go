package forward

import (
	"testing"
	"time"
)

func TestBuffer_AddAndDrain(t *testing.T) {
	buf := NewBuffer(1024)

	b1 := Batch{Labels: map[string]string{"a": "1"}, Size: 100}
	b2 := Batch{Labels: map[string]string{"b": "2"}, Size: 200}
	b3 := Batch{Labels: map[string]string{"c": "3"}, Size: 300}

	buf.Add(b1)
	buf.Add(b2)
	buf.Add(b3)

	if buf.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", buf.Len())
	}
	if buf.Size() != 600 {
		t.Fatalf("Size() = %d, want 600", buf.Size())
	}

	drained := buf.Drain()
	if len(drained) != 3 {
		t.Fatalf("drained %d batches, want 3", len(drained))
	}
	if drained[0].Labels["a"] != "1" {
		t.Error("expected FIFO order, first batch label mismatch")
	}
	if drained[2].Labels["c"] != "3" {
		t.Error("expected FIFO order, last batch label mismatch")
	}

	if buf.Len() != 0 {
		t.Errorf("Len() after drain = %d, want 0", buf.Len())
	}
	if buf.Size() != 0 {
		t.Errorf("Size() after drain = %d, want 0", buf.Size())
	}
}

func TestBuffer_Overflow(t *testing.T) {
	buf := NewBuffer(500)

	buf.Add(Batch{Labels: map[string]string{"a": "1"}, Size: 200})
	buf.Add(Batch{Labels: map[string]string{"b": "2"}, Size: 200})

	if buf.Drops() != 0 {
		t.Fatalf("drops = %d, want 0", buf.Drops())
	}

	// this should evict the first batch (200 + 200 + 200 = 600 > 500)
	buf.Add(Batch{Labels: map[string]string{"c": "3"}, Size: 200})

	if buf.Drops() != 1 {
		t.Fatalf("drops = %d, want 1", buf.Drops())
	}
	if buf.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", buf.Len())
	}

	drained := buf.Drain()
	if drained[0].Labels["b"] != "2" {
		t.Error("expected oldest (a) to be evicted, but first is not b")
	}
	if drained[1].Labels["c"] != "3" {
		t.Error("expected c to be second")
	}
}

func TestBuffer_DrainEmpty(t *testing.T) {
	buf := NewBuffer(1024)
	drained := buf.Drain()
	if drained != nil {
		t.Errorf("expected nil for empty drain, got %d batches", len(drained))
	}
}

func TestBuffer_SingleLargeBatch(t *testing.T) {
	buf := NewBuffer(100)

	// add a small batch first
	buf.Add(Batch{Labels: map[string]string{"small": "1"}, Size: 50})

	// add a batch larger than cap — should evict everything and still be added
	buf.Add(Batch{Labels: map[string]string{"large": "1"}, Size: 200})

	if buf.Drops() != 1 {
		t.Fatalf("drops = %d, want 1 (small batch evicted)", buf.Drops())
	}
	if buf.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", buf.Len())
	}

	drained := buf.Drain()
	if drained[0].Labels["large"] != "1" {
		t.Error("expected the large batch to be kept")
	}
}

func TestBuffer_SizeTracking(t *testing.T) {
	buf := NewBuffer(1000)

	buf.Add(Batch{Size: 100})
	buf.Add(Batch{Size: 200})
	if buf.Size() != 300 {
		t.Errorf("Size() = %d, want 300", buf.Size())
	}

	buf.Drain()
	if buf.Size() != 0 {
		t.Errorf("Size() after drain = %d, want 0", buf.Size())
	}
}

func TestEstimateBatchSize(t *testing.T) {
	labels := map[string]string{"app": "test", "env": "dev"}
	lines := []TimestampedLine{
		{Timestamp: time.Now(), Line: "hello world"},
		{Timestamp: time.Now(), Line: "another line"},
	}

	size := EstimateBatchSize(labels, lines)
	// labels: "app"(3) + "test"(4) + "env"(3) + "dev"(3) = 13
	// lines: "hello world"(11) + 24 + "another line"(12) + 24 = 71
	// total: 84
	if size != 84 {
		t.Errorf("EstimateBatchSize = %d, want 84", size)
	}
}
