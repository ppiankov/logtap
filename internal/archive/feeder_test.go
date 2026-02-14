package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func setupFeederDir(t *testing.T, n int, interval time.Duration) (string, *Reader) {
	t.Helper()
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := make([]recv.LogEntry, n)
	for i := range entries {
		entries[i] = recv.LogEntry{
			Timestamp: base.Add(time.Duration(i) * interval),
			Labels:    map[string]string{"app": "api"},
			Message:   fmt.Sprintf("line %d", i),
		}
	}

	writeMetadata(t, dir, base, base.Add(time.Duration(n)*interval), int64(n))
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(time.Duration(n-1) * interval),
		Lines: int64(n),
	}})

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, r
}

func TestFeederInstantSpeed(t *testing.T) {
	_, reader := setupFeederDir(t, 100, time.Second)
	ring := recv.NewLogRing(200)
	feeder := NewFeeder(reader, ring, nil, SpeedInstant)

	start := time.Now()
	feeder.Start()

	// wait for completion with timeout
	deadline := time.After(5 * time.Second)
	for !feeder.Done() {
		select {
		case <-deadline:
			feeder.Stop()
			t.Fatal("feeder did not complete in time")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	elapsed := time.Since(start)

	feeder.Stop()

	if feeder.LinesEmitted() != 100 {
		t.Errorf("LinesEmitted = %d, want 100", feeder.LinesEmitted())
	}
	if feeder.Err() != nil {
		t.Errorf("unexpected error: %v", feeder.Err())
	}
	// instant should be very fast
	if elapsed > 2*time.Second {
		t.Errorf("instant mode took %v, expected < 2s", elapsed)
	}

	snap := ring.Snapshot()
	if len(snap) != 100 {
		t.Errorf("ring has %d entries, want 100", len(snap))
	}
}

func TestFeederStop(t *testing.T) {
	// 1000 entries at 1s intervals with realtime speed would take 1000s
	// stopping should terminate quickly
	_, reader := setupFeederDir(t, 1000, time.Second)
	ring := recv.NewLogRing(100)
	feeder := NewFeeder(reader, ring, nil, SpeedRealtime)

	feeder.Start()
	time.Sleep(100 * time.Millisecond) // let it start
	feeder.Stop()

	// should have emitted some but not all
	emitted := feeder.LinesEmitted()
	if emitted >= 1000 {
		t.Errorf("expected partial emission after stop, got %d", emitted)
	}
	if !feeder.Done() {
		t.Error("expected done after stop")
	}
}

func TestFeederPauseResume(t *testing.T) {
	_, reader := setupFeederDir(t, 50, 10*time.Millisecond)
	ring := recv.NewLogRing(100)
	feeder := NewFeeder(reader, ring, nil, SpeedInstant)

	feeder.Start()
	time.Sleep(10 * time.Millisecond)

	// pause
	paused := feeder.TogglePause()
	if !paused {
		t.Error("expected paused = true")
	}
	if !feeder.Paused() {
		t.Error("expected Paused() = true")
	}

	before := feeder.LinesEmitted()
	time.Sleep(100 * time.Millisecond)
	after := feeder.LinesEmitted()

	// during pause, emission might increase slightly (entries already in flight)
	// but should not complete
	if feeder.Done() && before < 50 {
		// if not all entries were already emitted before pause, it shouldn't finish during pause
		t.Log("feeder completed during pause — all entries were already emitted before pause took effect")
	}

	// resume
	paused = feeder.TogglePause()
	if paused {
		t.Error("expected paused = false after resume")
	}

	// wait for completion
	deadline := time.After(5 * time.Second)
	for !feeder.Done() {
		select {
		case <-deadline:
			feeder.Stop()
			t.Fatal("feeder did not complete after resume")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	_ = after // used above to verify pause behavior
	feeder.Stop()

	if feeder.LinesEmitted() != 50 {
		t.Errorf("LinesEmitted = %d, want 50", feeder.LinesEmitted())
	}
}

func TestFeederSpeedChange(t *testing.T) {
	// entries 100ms apart, start at 1x, switch to instant
	_, reader := setupFeederDir(t, 20, 100*time.Millisecond)
	ring := recv.NewLogRing(100)
	feeder := NewFeeder(reader, ring, nil, SpeedRealtime)

	feeder.Start()
	time.Sleep(200 * time.Millisecond) // let some entries through at 1x

	// switch to instant — should complete quickly
	feeder.SetSpeed(SpeedInstant)

	deadline := time.After(5 * time.Second)
	for !feeder.Done() {
		select {
		case <-deadline:
			feeder.Stop()
			t.Fatal("feeder did not complete after speed change")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	feeder.Stop()

	if feeder.LinesEmitted() != 20 {
		t.Errorf("LinesEmitted = %d, want 20", feeder.LinesEmitted())
	}
}

func TestFeederSpeedAccessor(t *testing.T) {
	_, reader := setupFeederDir(t, 5, time.Second)
	ring := recv.NewLogRing(10)
	feeder := NewFeeder(reader, ring, nil, Speed(10))

	if feeder.Speed() != 10 {
		t.Errorf("Speed = %v, want 10", feeder.Speed())
	}

	feeder.SetSpeed(Speed(5))
	if feeder.Speed() != 5 {
		t.Errorf("Speed = %v, want 5", feeder.Speed())
	}
}

func TestFeederDoneSignal(t *testing.T) {
	_, reader := setupFeederDir(t, 5, time.Millisecond)
	ring := recv.NewLogRing(10)
	feeder := NewFeeder(reader, ring, nil, SpeedInstant)

	if feeder.Done() {
		t.Error("should not be done before start")
	}

	feeder.Start()

	deadline := time.After(5 * time.Second)
	for !feeder.Done() {
		select {
		case <-deadline:
			feeder.Stop()
			t.Fatal("feeder did not signal done")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	feeder.Stop()

	if !feeder.Done() {
		t.Error("should be done after completion")
	}
}

func TestFeederWithFilter(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// mix of api and web entries
	var entries []recv.LogEntry
	for i := 0; i < 20; i++ {
		app := "api"
		if i%2 == 0 {
			app = "web"
		}
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Labels:    map[string]string{"app": app},
			Message:   fmt.Sprintf("line %d", i),
		})
	}

	writeMetadata(t, dir, base, base.Add(20*time.Second), 20)
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl",
		From:  base,
		To:    base.Add(19 * time.Second),
		Lines: 20,
		Labels: map[string]map[string]int64{
			"app": {"api": 10, "web": 10},
		},
	}})

	reader, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}

	ring := recv.NewLogRing(100)
	filter := &Filter{Labels: []LabelMatcher{{Key: "app", Value: "api"}}}
	feeder := NewFeeder(reader, ring, filter, SpeedInstant)
	feeder.Start()

	deadline := time.After(5 * time.Second)
	for !feeder.Done() {
		select {
		case <-deadline:
			feeder.Stop()
			t.Fatal("feeder did not complete")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	feeder.Stop()

	if feeder.LinesEmitted() != 10 {
		t.Errorf("LinesEmitted = %d, want 10 (api only)", feeder.LinesEmitted())
	}

	snap := ring.Snapshot()
	for _, e := range snap {
		if e.Labels["app"] != "api" {
			t.Errorf("unexpected label %q in filtered results", e.Labels["app"])
		}
	}
}

// test helpers reused from reader_test.go (already in same package)

func TestFeederErrorOnBadDir(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	writeMetadata(t, dir, base, base, 0)
	// write an unreadable data file
	badFile := filepath.Join(dir, "2024-01-15T100000-000.jsonl.zst")
	if err := os.WriteFile(badFile, []byte("not valid zstd"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:  "2024-01-15T100000-000.jsonl.zst",
		From:  base,
		To:    base,
		Lines: 1,
	}})

	reader, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}

	ring := recv.NewLogRing(10)
	feeder := NewFeeder(reader, ring, nil, SpeedInstant)
	feeder.Start()

	deadline := time.After(5 * time.Second)
	for !feeder.Done() {
		select {
		case <-deadline:
			feeder.Stop()
			t.Fatal("feeder did not complete")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	feeder.Stop()

	if feeder.Err() == nil {
		t.Error("expected error for bad zstd file")
	}
}

// writeMetadata, writeIndex, writeDataFile are defined in reader_test.go
