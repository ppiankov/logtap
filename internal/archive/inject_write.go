package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

// InjectWriteResult describes the output of a fault-injection write.
type InjectWriteResult struct {
	Source        string `json:"source"`
	Output        string `json:"output"`
	OriginalLines int64  `json:"original_lines"`
	InjectedLines int64  `json:"injected_lines"`
	TotalLines    int64  `json:"total_lines"`
	Faults        int    `json:"faults_applied"`
}

// WriteJSON writes the result as JSON to w.
func (r *InjectWriteResult) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteText writes the result as human-readable text to w.
func (r *InjectWriteResult) WriteText(w io.Writer) {
	_, _ = fmt.Fprintf(w, "Injected %d faults into %s\n", r.Faults, r.Source)
	_, _ = fmt.Fprintf(w, "Original: %d lines, Injected: %d lines, Total: %d lines\n",
		r.OriginalLines, r.InjectedLines, r.TotalLines)
	_, _ = fmt.Fprintf(w, "Output: %s\n", r.Output)
}

// InjectWrite reads entries from a capture, applies fault injection,
// and writes the modified stream to a new capture directory.
func InjectWrite(src, dst string, filter *Filter, faults []FaultConfig) (*InjectWriteResult, error) {
	if src == dst {
		return nil, fmt.Errorf("source and destination cannot be the same")
	}

	reader, err := NewReader(src)
	if err != nil {
		return nil, fmt.Errorf("open source: %w", err)
	}
	meta := reader.Metadata()

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	injector := NewInjector(faults)

	// single output data file
	dataName := "injected-000.jsonl"
	dataPath := filepath.Join(dst, dataName)
	dataFile, err := os.Create(dataPath)
	if err != nil {
		return nil, fmt.Errorf("create data file: %w", err)
	}
	defer func() { _ = dataFile.Close() }()

	w := bufio.NewWriter(dataFile)

	var originalLines, totalLines int64
	var totalBytes int64
	var minTS, maxTS time.Time

	_, scanErr := reader.Scan(filter, func(e recv.LogEntry) bool {
		originalLines++
		entries := injector(e)

		for _, out := range entries {
			data, err := json.Marshal(out)
			if err != nil {
				continue
			}
			line := append(data, '\n')
			if _, err := w.Write(line); err != nil {
				return false
			}
			totalLines++
			totalBytes += int64(len(line))

			if minTS.IsZero() || out.Timestamp.Before(minTS) {
				minTS = out.Timestamp
			}
			if maxTS.IsZero() || out.Timestamp.After(maxTS) {
				maxTS = out.Timestamp
			}
		}
		return true
	})
	if scanErr != nil {
		return nil, fmt.Errorf("scan source: %w", scanErr)
	}

	if err := w.Flush(); err != nil {
		return nil, fmt.Errorf("flush data: %w", err)
	}

	// write metadata
	outMeta := &recv.Metadata{
		Version:    meta.Version,
		Format:     meta.Format,
		Started:    minTS,
		Stopped:    maxTS,
		TotalLines: totalLines,
		TotalBytes: totalBytes,
		LabelsSeen: meta.LabelsSeen,
		Redaction:  meta.Redaction,
	}
	if err := recv.WriteMetadata(dst, outMeta); err != nil {
		return nil, fmt.Errorf("write metadata: %w", err)
	}

	// write index
	indexEntries := []rotate.IndexEntry{
		{
			File:  dataName,
			From:  minTS,
			To:    maxTS,
			Lines: totalLines,
			Bytes: totalBytes,
		},
	}
	if err := writeInjectIndex(dst, indexEntries); err != nil {
		return nil, fmt.Errorf("write index: %w", err)
	}

	return &InjectWriteResult{
		Source:        src,
		Output:        dst,
		OriginalLines: originalLines,
		InjectedLines: totalLines - originalLines,
		TotalLines:    totalLines,
		Faults:        len(faults),
	}, nil
}

func writeInjectIndex(dir string, entries []rotate.IndexEntry) error {
	f, err := os.Create(filepath.Join(dir, "index.jsonl"))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
			return err
		}
	}
	return nil
}
