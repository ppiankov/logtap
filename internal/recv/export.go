package recv

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ExportFilteredLines writes a slice of log entries as a new capture directory.
// Returns the number of lines written.
func ExportFilteredLines(entries []LogEntry, dir string) (int64, error) {
	if len(entries) == 0 {
		return 0, fmt.Errorf("no lines to export")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create output directory: %w", err)
	}

	dataName := "export-000.jsonl"
	dataPath := filepath.Join(dir, dataName)
	dataFile, err := os.Create(dataPath)
	if err != nil {
		return 0, fmt.Errorf("create data file: %w", err)
	}
	defer func() { _ = dataFile.Close() }()

	w := bufio.NewWriter(dataFile)

	var totalBytes int64
	var minTS, maxTS time.Time

	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		line := append(data, '\n')
		if _, err := w.Write(line); err != nil {
			return 0, fmt.Errorf("write entry: %w", err)
		}
		totalBytes += int64(len(line))

		if minTS.IsZero() || e.Timestamp.Before(minTS) {
			minTS = e.Timestamp
		}
		if maxTS.IsZero() || e.Timestamp.After(maxTS) {
			maxTS = e.Timestamp
		}
	}

	if err := w.Flush(); err != nil {
		return 0, fmt.Errorf("flush data: %w", err)
	}

	totalLines := int64(len(entries))

	meta := &Metadata{
		Version:    1,
		Format:     "jsonl",
		Started:    minTS,
		Stopped:    maxTS,
		TotalLines: totalLines,
		TotalBytes: totalBytes,
	}
	if err := WriteMetadata(dir, meta); err != nil {
		return 0, fmt.Errorf("write metadata: %w", err)
	}

	// write index — use anonymous struct to avoid importing rotate (cycle)
	type indexEntry struct {
		File  string    `json:"file"`
		From  time.Time `json:"from"`
		To    time.Time `json:"to"`
		Lines int64     `json:"lines"`
		Bytes int64     `json:"bytes"`
	}

	f, err := os.Create(filepath.Join(dir, "index.jsonl"))
	if err != nil {
		return 0, fmt.Errorf("create index: %w", err)
	}
	defer func() { _ = f.Close() }()

	ie := indexEntry{
		File:  dataName,
		From:  minTS,
		To:    maxTS,
		Lines: totalLines,
		Bytes: totalBytes,
	}
	data, err := json.Marshal(ie)
	if err != nil {
		return 0, fmt.Errorf("marshal index: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
		return 0, fmt.Errorf("write index: %w", err)
	}

	return totalLines, nil
}
