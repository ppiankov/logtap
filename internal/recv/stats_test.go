package recv

import (
	"sync"
	"testing"
)

func TestStatsRecordEntry(t *testing.T) {
	s := NewStats()
	s.RecordEntry(map[string]string{"app": "api"})
	s.RecordEntry(map[string]string{"app": "api"})
	s.RecordEntry(map[string]string{"app": "worker"})

	snap := s.Snapshot(0, 0, 0)
	if snap.LogsReceived != 3 {
		t.Errorf("LogsReceived = %d, want 3", snap.LogsReceived)
	}
	if len(snap.Talkers) != 2 {
		t.Fatalf("Talkers len = %d, want 2", len(snap.Talkers))
	}
	// api should be first (higher count)
	if snap.Talkers[0].Name != "api" || snap.Talkers[0].Count != 2 {
		t.Errorf("top talker = %v, want api:2", snap.Talkers[0])
	}
	if snap.Talkers[1].Name != "worker" || snap.Talkers[1].Count != 1 {
		t.Errorf("second talker = %v, want worker:1", snap.Talkers[1])
	}
}

func TestStatsRecordDrop(t *testing.T) {
	s := NewStats()
	s.RecordDrop()
	s.RecordDrop()
	s.RecordDrop()

	snap := s.Snapshot(0, 0, 0)
	if snap.LogsDropped != 3 {
		t.Errorf("LogsDropped = %d, want 3", snap.LogsDropped)
	}
}

func TestStatsLabelFallback(t *testing.T) {
	s := NewStats()
	// no "app" label — should use first label value
	s.RecordEntry(map[string]string{"service": "gateway"})

	snap := s.Snapshot(0, 0, 0)
	if len(snap.Talkers) != 1 {
		t.Fatalf("Talkers len = %d, want 1", len(snap.Talkers))
	}
	if snap.Talkers[0].Name != "gateway" {
		t.Errorf("talker name = %q, want %q", snap.Talkers[0].Name, "gateway")
	}
}

func TestStatsEmptyLabels(t *testing.T) {
	s := NewStats()
	s.RecordEntry(map[string]string{})

	snap := s.Snapshot(0, 0, 0)
	if snap.LogsReceived != 1 {
		t.Errorf("LogsReceived = %d, want 1", snap.LogsReceived)
	}
	if len(snap.Talkers) != 0 {
		t.Errorf("Talkers len = %d, want 0", len(snap.Talkers))
	}
}

func TestStatsSnapshotDiskFields(t *testing.T) {
	s := NewStats()
	snap := s.Snapshot(1000, 5000, 500)
	if snap.DiskUsage != 1000 {
		t.Errorf("DiskUsage = %d, want 1000", snap.DiskUsage)
	}
	if snap.DiskCap != 5000 {
		t.Errorf("DiskCap = %d, want 5000", snap.DiskCap)
	}
	if snap.BytesWritten != 500 {
		t.Errorf("BytesWritten = %d, want 500", snap.BytesWritten)
	}
}

func TestStatsTalkersSortedDescending(t *testing.T) {
	s := NewStats()
	for i := 0; i < 10; i++ {
		s.RecordEntry(map[string]string{"app": "low"})
	}
	for i := 0; i < 100; i++ {
		s.RecordEntry(map[string]string{"app": "high"})
	}
	for i := 0; i < 50; i++ {
		s.RecordEntry(map[string]string{"app": "mid"})
	}

	snap := s.Snapshot(0, 0, 0)
	if len(snap.Talkers) != 3 {
		t.Fatalf("Talkers len = %d, want 3", len(snap.Talkers))
	}
	if snap.Talkers[0].Name != "high" {
		t.Errorf("first = %q, want high", snap.Talkers[0].Name)
	}
	if snap.Talkers[1].Name != "mid" {
		t.Errorf("second = %q, want mid", snap.Talkers[1].Name)
	}
	if snap.Talkers[2].Name != "low" {
		t.Errorf("third = %q, want low", snap.Talkers[2].Name)
	}
}

func TestStatsConcurrent(t *testing.T) {
	s := NewStats()
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				s.RecordEntry(map[string]string{"app": "svc"})
				s.RecordDrop()
				s.Snapshot(0, 0, 0)
			}
		}()
	}
	wg.Wait()

	snap := s.Snapshot(0, 0, 0)
	if snap.LogsReceived != 10000 {
		t.Errorf("LogsReceived = %d, want 10000", snap.LogsReceived)
	}
	if snap.LogsDropped != 10000 {
		t.Errorf("LogsDropped = %d, want 10000", snap.LogsDropped)
	}
}
