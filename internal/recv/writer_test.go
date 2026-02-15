package recv

import (
	"bytes"
	"testing"
	"time"
)

func TestBytesWritten(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(64, &buf, nil)

	entry := LogEntry{
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Message:   "test message",
	}
	if !w.Send(entry) {
		t.Error("send should succeed")
	}

	time.Sleep(50 * time.Millisecond)

	if w.BytesWritten() == 0 {
		t.Error("BytesWritten should be > 0")
	}
	if w.LinesWritten() != 1 {
		t.Errorf("LinesWritten: got %d, want 1", w.LinesWritten())
	}
	w.Close()
}

func TestWriterWithTracker(t *testing.T) {
	var buf bytes.Buffer
	var trackedLabels map[string]string
	tracker := func(ts time.Time, labels map[string]string) {
		trackedLabels = labels
	}

	w := NewWriter(64, &buf, tracker)
	entry := LogEntry{
		Timestamp: time.Now(),
		Labels:    map[string]string{"app": "test"},
		Message:   "tracked",
	}
	w.Send(entry)
	time.Sleep(50 * time.Millisecond)
	w.Close()

	if trackedLabels == nil || trackedLabels["app"] != "test" {
		t.Errorf("tracker not called correctly: %v", trackedLabels)
	}
}

func TestWriterDropsWhenFull(t *testing.T) {
	var buf bytes.Buffer
	// buffer size 1, block the drain by not reading
	w := NewWriter(1, &buf, nil)

	// fill the channel
	entry := LogEntry{Timestamp: time.Now(), Message: "fill"}
	w.Send(entry) // this might succeed
	// keep sending until one is dropped
	var dropped bool
	for i := 0; i < 100; i++ {
		if !w.Send(entry) {
			dropped = true
			break
		}
	}
	w.Close()

	if !dropped {
		t.Error("expected at least one drop with buffer size 1")
	}
}

func TestWriterDoubleClose(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(64, &buf, nil)
	w.Close()
	w.Close() // should not panic
}
