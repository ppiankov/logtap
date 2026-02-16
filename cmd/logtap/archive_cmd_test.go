package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func redirectOutput(t *testing.T) func() {
	t.Helper()

	stdout := os.Stdout
	stderr := os.Stderr

	outFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create stdout temp: %v", err)
	}
	errFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("create stderr temp: %v", err)
	}

	os.Stdout = outFile
	os.Stderr = errFile

	return func() {
		os.Stdout = stdout
		os.Stderr = stderr
		_ = outFile.Close()
		_ = errFile.Close()
	}
}

func writeIndex(t *testing.T, dir string, entries []rotate.IndexEntry) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, "index.jsonl"))
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	defer func() { _ = f.Close() }()

	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal index: %v", err)
		}
		if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
			t.Fatalf("write index: %v", err)
		}
	}
}

func writeDataFile(t *testing.T, dir, name string, entries []recv.LogEntry) int64 {
	t.Helper()

	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create data file: %v", err)
	}
	defer func() { _ = f.Close() }()

	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
			t.Fatalf("write entry: %v", err)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat data file: %v", err)
	}
	return info.Size()
}

func makeCaptureDir(t *testing.T, entries []recv.LogEntry) string {
	t.Helper()
	if len(entries) == 0 {
		t.Fatal("entries cannot be empty")
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	dir := t.TempDir()
	name := entries[0].Timestamp.UTC().Format("2006-01-02T150405") + "-000.jsonl"
	bytes := writeDataFile(t, dir, name, entries)

	labels := make(map[string]map[string]int64)
	labelKeys := make(map[string]bool)
	for _, e := range entries {
		for k, v := range e.Labels {
			if labels[k] == nil {
				labels[k] = make(map[string]int64)
			}
			labels[k][v]++
			labelKeys[k] = true
		}
	}

	writeIndex(t, dir, []rotate.IndexEntry{
		{
			File:   name,
			From:   entries[0].Timestamp,
			To:     entries[len(entries)-1].Timestamp,
			Lines:  int64(len(entries)),
			Bytes:  bytes,
			Labels: labels,
		},
	})

	keys := make([]string, 0, len(labelKeys))
	for k := range labelKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	meta := &recv.Metadata{
		Version:    1,
		Format:     "jsonl",
		Started:    entries[0].Timestamp,
		Stopped:    entries[len(entries)-1].Timestamp,
		TotalLines: int64(len(entries)),
		TotalBytes: bytes,
		LabelsSeen: keys,
	}
	if err := recv.WriteMetadata(dir, meta); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	return dir
}

func sampleEntries(base time.Time) []recv.LogEntry {
	return []recv.LogEntry{
		{
			Timestamp: base,
			Labels:    map[string]string{"app": "web"},
			Message:   "hello world",
		},
		{
			Timestamp: base.Add(2 * time.Second),
			Labels:    map[string]string{"app": "web"},
			Message:   "error: boom",
		},
	}
}

func TestRunInspect_Success(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	restore := redirectOutput(t)
	defer restore()

	t.Run("text", func(t *testing.T) {
		if err := runInspect(dir, false); err != nil {
			t.Fatalf("runInspect text: %v", err)
		}
	})

	t.Run("json", func(t *testing.T) {
		if err := runInspect(dir, true); err != nil {
			t.Fatalf("runInspect json: %v", err)
		}
	})
}

func TestRunDiff_Success(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	dirA := makeCaptureDir(t, sampleEntries(base))
	dirB := makeCaptureDir(t, sampleEntries(base.Add(5*time.Second)))

	restore := redirectOutput(t)
	defer restore()

	t.Run("text", func(t *testing.T) {
		if err := runDiff(dirA, dirB, false); err != nil {
			t.Fatalf("runDiff text: %v", err)
		}
	})

	t.Run("json", func(t *testing.T) {
		if err := runDiff(dirA, dirB, true); err != nil {
			t.Fatalf("runDiff json: %v", err)
		}
	})
}

func TestRunGrep_Success(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	restore := redirectOutput(t)
	defer restore()

	t.Run("matches", func(t *testing.T) {
		if err := runGrep("error", dir, "", "", nil, false); err != nil {
			t.Fatalf("runGrep: %v", err)
		}
	})

	t.Run("count", func(t *testing.T) {
		if err := runGrep("error", dir, "", "", nil, true); err != nil {
			t.Fatalf("runGrep count: %v", err)
		}
	})
}

func TestRunSlice_Success(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outDir := filepath.Join(t.TempDir(), "slice")

	restore := redirectOutput(t)
	defer restore()

	if err := runSlice(dir, "", "", nil, "", outDir); err != nil {
		t.Fatalf("runSlice: %v", err)
	}
}

func TestRunExport_Success(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outPath := filepath.Join(t.TempDir(), "export.jsonl")

	restore := redirectOutput(t)
	defer restore()

	if err := runExport(dir, "jsonl", "", "", nil, "", outPath); err != nil {
		t.Fatalf("runExport: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("export output missing: %v", err)
	}
}

func TestRunMerge_Success(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	dirA := makeCaptureDir(t, sampleEntries(base))
	dirB := makeCaptureDir(t, sampleEntries(base.Add(10*time.Second)))
	outDir := filepath.Join(t.TempDir(), "merged")

	restore := redirectOutput(t)
	defer restore()

	if err := runMerge([]string{dirA, dirB}, outDir); err != nil {
		t.Fatalf("runMerge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "metadata.json")); err != nil {
		t.Fatalf("merged metadata missing: %v", err)
	}
}

func TestRunSnapshot_Success(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	archivePath := filepath.Join(t.TempDir(), "capture.tar.zst")
	extractDir := filepath.Join(t.TempDir(), "extract")

	restore := redirectOutput(t)
	defer restore()

	if err := runSnapshot(dir, archivePath, false); err != nil {
		t.Fatalf("runSnapshot pack: %v", err)
	}
	if err := runSnapshot(archivePath, extractDir, true); err != nil {
		t.Fatalf("runSnapshot extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(extractDir, "metadata.json")); err != nil {
		t.Fatalf("extracted metadata missing: %v", err)
	}
}

func TestRunTriage_Success(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	t.Run("json", func(t *testing.T) {
		restore := redirectOutput(t)
		defer restore()

		if err := runTriage(dir, "", 1, time.Minute, 5, true, false); err != nil {
			t.Fatalf("runTriage json: %v", err)
		}
	})

	t.Run("files", func(t *testing.T) {
		restore := redirectOutput(t)
		defer restore()

		outDir := filepath.Join(t.TempDir(), "triage")
		if err := runTriage(dir, outDir, 1, time.Minute, 5, false, false); err != nil {
			t.Fatalf("runTriage files: %v", err)
		}
		if _, err := os.Stat(filepath.Join(outDir, "summary.md")); err != nil {
			t.Fatalf("triage summary missing: %v", err)
		}
	})
}

func TestCompletionCommand_RunE(t *testing.T) {
	shells := []string{"bash", "zsh", "fish"}

	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			restore := redirectOutput(t)
			defer restore()

			root := &cobra.Command{Use: "logtap"}
			root.AddCommand(newCompletionCmd())
			root.SetArgs([]string{"completion", shell})
			if err := root.Execute(); err != nil {
				t.Fatalf("completion %s: %v", shell, err)
			}
		})
	}
}
