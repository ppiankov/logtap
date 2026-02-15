package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func BenchmarkFilterMatchEntry(b *testing.B) {
	f := &Filter{
		From:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		To:     time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
		Labels: []LabelMatcher{{Key: "app", Value: "api"}},
		Grep:   regexp.MustCompile("error"),
	}
	entry := recv.LogEntry{
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Labels:    map[string]string{"app": "api", "env": "prod"},
		Message:   "error: connection refused to database",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.MatchEntry(entry)
	}
}

func BenchmarkFilterMatchEntryNoGrep(b *testing.B) {
	f := &Filter{
		From:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		To:     time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
		Labels: []LabelMatcher{{Key: "app", Value: "api"}},
	}
	entry := recv.LogEntry{
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Labels:    map[string]string{"app": "api", "env": "prod"},
		Message:   "request processed successfully",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.MatchEntry(entry)
	}
}

func BenchmarkFilterSkipFile(b *testing.B) {
	f := &Filter{
		From: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		To:   time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
		Labels: []LabelMatcher{
			{Key: "app", Value: "api"},
			{Key: "env", Value: "prod"},
		},
	}
	idx := &rotate.IndexEntry{
		From:  time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC),
		To:    time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC),
		Lines: 1000,
		Labels: map[string]map[string]int64{
			"app": {"api": 500, "web": 500},
			"env": {"prod": 800, "staging": 200},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.SkipFile(idx)
	}
}

func setupBenchCapture(b *testing.B, n int) string {
	b.Helper()
	dir := b.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	entries := makeEntries(n, base, "api")

	// write metadata
	meta := recv.Metadata{Version: 1, Format: "jsonl", Started: base, Stopped: base.Add(time.Duration(n) * time.Second), TotalLines: int64(n)}
	data, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644)

	// write data file
	var raw []byte
	for _, e := range entries {
		d, _ := json.Marshal(e)
		raw = append(raw, d...)
		raw = append(raw, '\n')
	}
	_ = os.WriteFile(filepath.Join(dir, "2024-01-15T100000-000.jsonl"), raw, 0o644)

	// write index
	idx := rotate.IndexEntry{
		File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(time.Duration(n-1) * time.Second), Lines: int64(n),
	}
	idxData, _ := json.Marshal(idx)
	_ = os.WriteFile(filepath.Join(dir, "index.jsonl"), append(idxData, '\n'), 0o644)

	return dir
}

func BenchmarkReaderScan(b *testing.B) {
	dir := setupBenchCapture(b, 10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := NewReader(dir)
		if err != nil {
			b.Fatal(err)
		}
		_, err = r.Scan(nil, func(e recv.LogEntry) bool {
			return true
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReaderScanWithFilter(b *testing.B) {
	dir := setupBenchCapture(b, 10000)
	f := &Filter{Grep: regexp.MustCompile("line [0-9]*5$")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := NewReader(dir)
		if err != nil {
			b.Fatal(err)
		}
		_, err = r.Scan(f, func(e recv.LogEntry) bool {
			return true
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTriageScan(b *testing.B) {
	dir := b.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	var entries []recv.LogEntry
	for i := 0; i < 10000; i++ {
		msg := fmt.Sprintf("request %d processed", i)
		if i%10 == 0 {
			msg = fmt.Sprintf("error: connection timeout for request %d", i)
		}
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Labels:    map[string]string{"app": "api"},
			Message:   msg,
		})
	}

	meta := recv.Metadata{Version: 1, Format: "jsonl", Started: base, Stopped: base.Add(10000 * time.Second), TotalLines: 10000}
	data, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644)

	var raw []byte
	for _, e := range entries {
		d, _ := json.Marshal(e)
		raw = append(raw, d...)
		raw = append(raw, '\n')
	}
	_ = os.WriteFile(filepath.Join(dir, "2024-01-15T100000-000.jsonl"), raw, 0o644)

	idx := rotate.IndexEntry{File: "2024-01-15T100000-000.jsonl", From: base, To: base.Add(9999 * time.Second), Lines: 10000}
	idxData, _ := json.Marshal(idx)
	_ = os.WriteFile(filepath.Join(dir, "index.jsonl"), append(idxData, '\n'), 0o644)

	cfg := TriageConfig{Jobs: 1, Top: 10}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Triage(dir, cfg, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}
