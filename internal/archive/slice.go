package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// LabelFilter represents a key-value pair for label filtering.
type LabelFilter struct {
	Key   string
	Value string
}

// SliceOptions holds the parameters for the slicing operation.
type SliceOptions struct {
	From       time.Time
	To         time.Time
	Labels     []LabelFilter
	Grep       *regexp.Regexp
	OutputDir  string
	CaptureDir string
}

// logEntry represents a minimal structure to parse the timestamp from a log line.
type logEntry struct {
	Timestamp string `json:"ts"`
}

// Slice performs the slicing operation.
func Slice(opts SliceOptions) error {
	if opts.CaptureDir == "" {
		return fmt.Errorf("capture directory cannot be empty")
	}
	if opts.OutputDir == "" {
		return fmt.Errorf("output directory cannot be empty")
	}
	if opts.OutputDir == opts.CaptureDir {
		return fmt.Errorf("output directory cannot be the same as capture directory")
	}

	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	sourceMeta, err := ReadMetadata(opts.CaptureDir)
	if err != nil {
		return fmt.Errorf("read source metadata: %w", err)
	}

	sourceIndex, err := ReadIndex(opts.CaptureDir)
	if err != nil {
		return fmt.Errorf("read source index: %w", err)
	}

	newMeta := NewMetadata()
	newMeta.Version = sourceMeta.Version
	newMeta.Format = sourceMeta.Format
	newMeta.Redaction = sourceMeta.Redaction

	newIndex := NewIndex()

	var totalLines int64
	var totalBytes int64
	var minTS, maxTS time.Time

	filtered := filterIndexEntries(sourceIndex.Entries, opts)
	timeFilterActive := !opts.From.IsZero() || !opts.To.IsZero()

	for _, ie := range filtered {
		srcPath := filepath.Join(opts.CaptureDir, ie.File)
		outPath := filepath.Join(opts.OutputDir, ie.File)

		lines, bytes, fileMinTS, fileMaxTS, err := sliceFile(srcPath, outPath, opts, timeFilterActive)
		if err != nil {
			return fmt.Errorf("slice %s: %w", ie.File, err)
		}

		if lines > 0 {
			newIndex.Entries = append(newIndex.Entries, IndexEntry{
				File:  ie.File,
				From:  fileMinTS,
				To:    fileMaxTS,
				Lines: lines,
				Bytes: bytes,
			})
			totalLines += lines
			totalBytes += bytes

			if minTS.IsZero() || fileMinTS.Before(minTS) {
				minTS = fileMinTS
			}
			if maxTS.IsZero() || fileMaxTS.After(maxTS) {
				maxTS = fileMaxTS
			}
		} else {
			_ = os.Remove(outPath)
		}

		fmt.Printf("Processed file: %s (Matched %d lines)\n", ie.File, lines)
	}

	if totalLines == 0 {
		_ = os.RemoveAll(opts.OutputDir)
		return fmt.Errorf("no matching log lines found for the given filters")
	}

	newMeta.Started = minTS
	newMeta.Stopped = maxTS
	newMeta.TotalLines = totalLines
	newMeta.TotalBytes = totalBytes
	if err := WriteMetadata(opts.OutputDir, newMeta); err != nil {
		return fmt.Errorf("write output metadata: %w", err)
	}

	if err := WriteIndex(opts.OutputDir, newIndex); err != nil {
		return fmt.Errorf("write output index: %w", err)
	}

	fmt.Printf("Slicing complete. Wrote %d lines across %d files to %s\n", totalLines, len(newIndex.Entries), opts.OutputDir)
	return nil
}

// sliceFile reads a single data file, applies filters, and writes matched lines to outPath.
// Handles both plain .jsonl and compressed .jsonl.zst files.
func sliceFile(srcPath, outPath string, opts SliceOptions, timeFilterActive bool) (lines, bytes int64, minTS, maxTS time.Time, err error) {
	inFile, err := os.Open(srcPath)
	if err != nil {
		return 0, 0, minTS, maxTS, fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = inFile.Close() }()

	var reader io.Reader = inFile
	if strings.HasSuffix(srcPath, ".zst") {
		dec, zstdErr := zstd.NewReader(inFile)
		if zstdErr != nil {
			return 0, 0, minTS, maxTS, fmt.Errorf("zstd open: %w", zstdErr)
		}
		defer dec.Close()
		reader = dec
	}

	outFile, err := os.Create(outPath)
	if err != nil {
		return 0, 0, minTS, maxTS, fmt.Errorf("create output: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	// Output is always written compressed if the source was compressed,
	// plain otherwise, to preserve the original format.
	compressed := strings.HasSuffix(srcPath, ".zst")
	var writer io.Writer = outFile
	if compressed {
		zw, zwErr := zstd.NewWriter(outFile)
		if zwErr != nil {
			return 0, 0, minTS, maxTS, fmt.Errorf("zstd writer: %w", zwErr)
		}
		defer func() { _ = zw.Close() }()
		writer = zw
	}

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		lineBytes := scanner.Bytes()
		match := true
		var ts time.Time

		var entry logEntry
		if unmarshalErr := json.Unmarshal(lineBytes, &entry); unmarshalErr != nil {
			if timeFilterActive {
				match = false
			}
		} else {
			ts, err = time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				if timeFilterActive {
					match = false
				}
			} else {
				if !opts.From.IsZero() && ts.Before(opts.From) {
					match = false
				}
				if !opts.To.IsZero() && (ts.After(opts.To) || ts.Equal(opts.To)) {
					match = false
				}
				if match {
					if minTS.IsZero() || ts.Before(minTS) {
						minTS = ts
					}
					if maxTS.IsZero() || ts.After(maxTS) {
						maxTS = ts
					}
				}
			}
		}

		if match && opts.Grep != nil && !opts.Grep.Match(lineBytes) {
			match = false
		}

		if match {
			if _, writeErr := writer.Write(append(lineBytes, '\n')); writeErr != nil {
				return 0, 0, minTS, maxTS, fmt.Errorf("write line: %w", writeErr)
			}
			lines++
			bytes += int64(len(lineBytes) + 1)
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return 0, 0, minTS, maxTS, fmt.Errorf("scan: %w", scanErr)
	}

	return lines, bytes, minTS, maxTS, nil
}

// filterIndexEntries filters index entries based on time and label criteria.
func filterIndexEntries(entries []IndexEntry, opts SliceOptions) []IndexEntry {
	var filtered []IndexEntry
	for _, entry := range entries {
		if !opts.From.IsZero() && entry.To.Before(opts.From) {
			continue
		}
		if !opts.To.IsZero() && entry.From.After(opts.To) {
			continue
		}

		if len(opts.Labels) > 0 {
			labelMatch := false
			for _, filter := range opts.Labels {
				if entry.Labels != nil {
					if values, ok := entry.Labels[filter.Key]; ok {
						if _, ok := values[filter.Value]; ok {
							labelMatch = true
							break
						}
					}
				}
			}
			if !labelMatch {
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
