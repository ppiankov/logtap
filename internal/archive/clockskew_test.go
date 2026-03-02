package archive

import (
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func makeSkewCapture(t *testing.T, base time.Time, app string, errors []string, offset time.Duration) string {
	t.Helper()
	dir := t.TempDir()

	entries := make([]recv.LogEntry, 0, len(errors)+5)
	for i := 0; i < 5; i++ {
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(offset + time.Duration(i)*time.Second),
			Labels:    map[string]string{"app": app},
			Message:   "info: normal operation",
		})
	}
	for i, msg := range errors {
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(offset + time.Duration(10+i)*time.Second),
			Labels:    map[string]string{"app": app},
			Message:   msg,
		})
	}

	name := "data-000.jsonl"
	writeDataFile(t, dir, name, entries)

	started := entries[0].Timestamp
	stopped := entries[len(entries)-1].Timestamp
	writeMetadata(t, dir, started, stopped, int64(len(entries)))
	writeIndex(t, dir, []rotate.IndexEntry{{
		File:   name,
		From:   started,
		To:     stopped,
		Lines:  int64(len(entries)),
		Labels: map[string]map[string]int64{"app": {app: int64(len(entries))}},
	}})

	return dir
}

func TestDetectSkew_SharedSignatures(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	skew := 3 * time.Second

	// reference: more lines (errors + normal)
	ref := makeSkewCapture(t, base, "api",
		[]string{"ERROR: connection refused", "ERROR: timeout exceeded"}, 0)
	// skewed source: same errors but shifted by 3s
	src := makeSkewCapture(t, base, "api",
		[]string{"ERROR: connection refused", "ERROR: timeout exceeded"}, skew)

	corrections, err := DetectSkew([]string{ref, src})
	if err != nil {
		t.Fatal(err)
	}

	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}

	cc := corrections[0]
	if cc.Source != src {
		t.Errorf("source = %q, want %q", cc.Source, src)
	}
	if cc.Method != "error_signature" {
		t.Errorf("method = %q, want %q", cc.Method, "error_signature")
	}
	// offset should be approximately -3000ms (ref - src = -skew)
	if cc.OffsetMs > 0 || cc.OffsetMs < -5000 {
		t.Errorf("offset = %dms, expected around -3000ms", cc.OffsetMs)
	}
	if cc.Confidence < 0.5 {
		t.Errorf("confidence = %.2f, expected >= 0.5", cc.Confidence)
	}
}

func TestDetectSkew_NoSharedSignatures(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	// different errors, no shared signatures → metadata fallback
	ref := makeSkewCapture(t, base, "api",
		[]string{"ERROR: connection refused"}, 0)
	src := makeSkewCapture(t, base, "web",
		[]string{"ERROR: disk full"}, 2*time.Second)

	corrections, err := DetectSkew([]string{ref, src})
	if err != nil {
		t.Fatal(err)
	}

	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}

	cc := corrections[0]
	if cc.Method != "metadata_overlap" {
		t.Errorf("method = %q, want %q", cc.Method, "metadata_overlap")
	}
	if cc.Confidence != 0.3 {
		t.Errorf("confidence = %.2f, want 0.3", cc.Confidence)
	}
}

func TestDetectSkew_ExceedsMax(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	// 2-minute skew exceeds 60s max
	ref := makeSkewCapture(t, base, "api",
		[]string{"ERROR: connection refused"}, 0)
	src := makeSkewCapture(t, base, "api",
		[]string{"ERROR: connection refused"}, 2*time.Minute)

	corrections, err := DetectSkew([]string{ref, src})
	if err != nil {
		t.Fatal(err)
	}

	// should be empty — skew too large
	if len(corrections) != 0 {
		t.Errorf("expected 0 corrections (skew too large), got %d", len(corrections))
	}
}

func TestDetectSkew_IdenticalClocks(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	ref := makeSkewCapture(t, base, "api",
		[]string{"ERROR: connection refused"}, 0)
	src := makeSkewCapture(t, base, "api",
		[]string{"ERROR: connection refused"}, 0)

	corrections, err := DetectSkew([]string{ref, src})
	if err != nil {
		t.Fatal(err)
	}

	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].OffsetMs != 0 {
		t.Errorf("offset = %dms, want 0", corrections[0].OffsetMs)
	}
}

func TestRewriteWithOffset(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	src := makeSkewCapture(t, base, "api", []string{"ERROR: boom"}, 0)
	dst := t.TempDir()

	offset := 5 * time.Second
	lines, err := RewriteWithOffset(src, dst, offset)
	if err != nil {
		t.Fatal(err)
	}
	if lines != 6 { // 5 normal + 1 error
		t.Errorf("lines = %d, want 6", lines)
	}

	// verify timestamps are shifted
	reader, err := NewReader(dst)
	if err != nil {
		t.Fatal(err)
	}

	var entries []recv.LogEntry
	_, err = reader.Scan(nil, func(e recv.LogEntry) bool {
		entries = append(entries, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := base.Add(offset)
	if !entries[0].Timestamp.Equal(expected) {
		t.Errorf("first timestamp = %v, want %v", entries[0].Timestamp, expected)
	}
}

func TestRewriteWithOffset_Negative(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	src := makeSkewCapture(t, base, "api", []string{"ERROR: boom"}, 0)
	dst := t.TempDir()

	offset := -3 * time.Second
	lines, err := RewriteWithOffset(src, dst, offset)
	if err != nil {
		t.Fatal(err)
	}
	if lines != 6 {
		t.Errorf("lines = %d, want 6", lines)
	}

	reader, err := NewReader(dst)
	if err != nil {
		t.Fatal(err)
	}

	var entries []recv.LogEntry
	_, err = reader.Scan(nil, func(e recv.LogEntry) bool {
		entries = append(entries, e)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := base.Add(offset)
	if !entries[0].Timestamp.Equal(expected) {
		t.Errorf("first timestamp = %v, want %v", entries[0].Timestamp, expected)
	}
}
