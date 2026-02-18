package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// 1. Validate input and setup output directory
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
		return fmt.Errorf("failed to create output directory %s: %w", opts.OutputDir, err)
	}

	// 2. Read source metadata and index
	sourceMeta, err := ReadMetadata(opts.CaptureDir)
	if err != nil {
		return fmt.Errorf("failed to read source metadata: %w", err)
	}

	sourceIndex, err := ReadIndex(opts.CaptureDir)
	if err != nil {
		return fmt.Errorf("failed to read source index: %w", err)
	}

	// Initialize new metadata and index for the output
	newMeta := NewMetadata()
	newMeta.Version = sourceMeta.Version
	newMeta.Format = sourceMeta.Format
	// newMeta.Started and newMeta.Stopped will be determined from actual log lines
	newMeta.Redaction = sourceMeta.Redaction

	newIndex := NewIndex()

	var totalLinesProcessed int64
	var totalBytesProcessed int64
	var minTimestamp = time.Time{}
	var maxTimestamp = time.Time{}

	// 3. Filter index entries based on time and labels
	filteredIndexEntries := filterIndexEntries(sourceIndex.Entries, opts)

	// 4. Process relevant files
	for _, entry := range filteredIndexEntries {
		sourceFilePath := filepath.Join(opts.CaptureDir, entry.File)
		outputFilePath := filepath.Join(opts.OutputDir, entry.File)

		// Create output file
		outFile, err := os.Create(outputFilePath)
		if err != nil {
			return fmt.Errorf("failed to create output file %s: %w", outputFilePath, err)
		}
		// Don't defer outFile.Close() here, close explicitly per file
		// Also don't defer zstdWriter.Close() for the same reason.
		
		zstdWriter, err := zstd.NewWriter(outFile)
		if err != nil {
			outFile.Close() // Close file if writer creation fails
			return fmt.Errorf("failed to create zstd writer for %s: %w", outputFilePath, err)
		}

		// Open source file
		inFile, err := os.Open(sourceFilePath)
		if err != nil {
			zstdWriter.Close()
			outFile.Close()
			return fmt.Errorf("failed to open source file %s: %w", sourceFilePath, err)
		}
		// Don't defer inFile.Close() here, close explicitly per file

		zstdReader, err := zstd.NewReader(inFile)
		if err != nil {
			inFile.Close()
			zstdWriter.Close()
			outFile.Close()
			return fmt.Errorf("failed to create zstd reader for %s: %w", sourceFilePath, err)
		}
		// Don't defer zstdReader.Close() here, close explicitly per file

		scanner := bufio.NewScanner(zstdReader)
		var currentFileLines int64
		var currentFileBytes int64
		var currentFileMinTimestamp = time.Time{}
		var currentFileMaxTimestamp = time.Time{}
		        var currentFileLabels map[string]map[string]int
		
				// Helper to track if time filters are active for malformed lines
				timeFilterActive := !opts.From.IsZero() || !opts.To.IsZero()
		
				for scanner.Scan() {
					lineBytes := scanner.Bytes()
					match := true
					var ts time.Time // Declare ts here for broader scope within the loop
		
					var entry logEntry
									if err := json.Unmarshal(lineBytes, &entry); err != nil {
										// If we can't parse timestamp, we can't filter by time.
										// For now, treat as no match if time filter is active.
										// TODO: Decide how to handle malformed lines, perhaps emit warning.
										if timeFilterActive {
											match = false // If time filters are active, malformed lines can't match.
										}
										// If no time filters, malformed lines can still pass if grep matches
									} else {
										ts, err = time.Parse(time.RFC3339, entry.Timestamp)
										if err != nil {
											if timeFilterActive {
												match = false // If time filters are active, unparseable lines can't match.
											}
										} else {
											// Apply From/To time filters
											if !opts.From.IsZero() && ts.Before(opts.From) {
												match = false
											}
											if !opts.To.IsZero() && (ts.After(opts.To) || ts.Equal(opts.To)) {
												match = false
											}							// Update min/max timestamps for the current file and overall
							if match {
								if currentFileMinTimestamp.IsZero() || ts.Before(currentFileMinTimestamp) {
									currentFileMinTimestamp = ts
								}
								if currentFileMaxTimestamp.IsZero() || ts.After(currentFileMaxTimestamp) {
									currentFileMaxTimestamp = ts
								}
							}
						}
					}
		
					// Apply grep filter
					if match && opts.Grep != nil {
						if !opts.Grep.Match(lineBytes) {
							match = false
						}
					}
		
					if match {
						if _, err := zstdWriter.Write(append(lineBytes, '\n')); err != nil {
							zstdReader.Close()
							inFile.Close()
							zstdWriter.Close()
							outFile.Close()
							return fmt.Errorf("failed to write line to output file %s: %w", outputFilePath, err)
						}
						currentFileLines++
						currentFileBytes += int64(len(lineBytes) + 1) // +1 for newline
		
						// TODO: Re-aggregate labels for the new index entry
					}
				}
		// Close resources for current file
		zstdReader.Close()
		inFile.Close()
		zstdWriter.Close()
		outFile.Close()

		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading source file %s: %w", sourceFilePath, err)
		}

		if currentFileLines > 0 {
			// Update newIndex with a new entry for the sliced file
			newEntry := IndexEntry{
				File:  entry.File,
				From:  currentFileMinTimestamp,
				To:    currentFileMaxTimestamp,
				Lines: currentFileLines,
				Bytes: currentFileBytes,
				Labels: currentFileLabels, // Will be nil for now, needs re-aggregation
			}
			newIndex.Entries = append(newIndex.Entries, newEntry)
			totalLinesProcessed += currentFileLines
			totalBytesProcessed += currentFileBytes

			if minTimestamp.IsZero() || currentFileMinTimestamp.Before(minTimestamp) {
				minTimestamp = currentFileMinTimestamp
			}
			if maxTimestamp.IsZero() || currentFileMaxTimestamp.After(maxTimestamp) {
				maxTimestamp = currentFileMaxTimestamp
			}

		} else {
			// If no lines matched, delete the empty output file
			_ = os.Remove(outputFilePath)
		}

		fmt.Printf("Processed file: %s (Matched %d lines)\n", entry.File, currentFileLines)
	}

	if totalLinesProcessed == 0 { // Check total lines processed, not total files
		_ = os.RemoveAll(opts.OutputDir) // Clean up empty output directory
		return fmt.Errorf("no matching log lines found for the given filters")
	}

	// 5. Update and write new metadata
	newMeta.Started = minTimestamp
	newMeta.Stopped = maxTimestamp
	newMeta.TotalLines = totalLinesProcessed
	newMeta.TotalBytes = totalBytesProcessed
	// newMeta.LabelsSeen = ... // Needs to be re-aggregated from newIndex.Entries
	if err := WriteMetadata(opts.OutputDir, newMeta); err != nil {
		return fmt.Errorf("failed to write output metadata: %w", err)
	}

	// 6. Write new index
	if err := WriteIndex(opts.OutputDir, newIndex); err != nil {
		return fmt.Errorf("failed to write output index to %s: %w", opts.OutputDir, err)
	}

	fmt.Printf("Slicing complete. Wrote %d lines across %d files to %s\n", totalLinesProcessed, len(newIndex.Entries), opts.OutputDir)
	return nil
}

// filterIndexEntries filters index entries based on time and label criteria.
func filterIndexEntries(entries []IndexEntry, opts SliceOptions) []IndexEntry {
	var filtered []IndexEntry
	for _, entry := range entries {
		// Time filter (file level)
		if !opts.From.IsZero() && entry.To.Before(opts.From) {
			continue
		}
		if !opts.To.IsZero() && entry.From.After(opts.To) {
			continue
		}

		// Label filter (file level)
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