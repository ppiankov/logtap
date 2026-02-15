package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/ppiankov/logtap/internal/recv"
)

// GrepConfig controls grep behavior.
type GrepConfig struct {
	CountOnly bool // only report per-file counts, do not call onMatch
}

// GrepMatch represents a matching entry with file context.
type GrepMatch struct {
	File  string
	Entry recv.LogEntry
}

// GrepProgress reports progress during grep scanning.
type GrepProgress struct {
	Scanned int64
	Total   int64
	Matches int64
}

// GrepFileCount holds the match count for a single file.
type GrepFileCount struct {
	File  string
	Count int64
}

// Grep searches a capture directory for entries matching the filter.
// In default mode, calls onMatch for each matching entry.
// Returns per-file counts (only files with matches) and any error.
func Grep(src string, filter *Filter, cfg GrepConfig,
	onMatch func(GrepMatch), progress func(GrepProgress)) ([]GrepFileCount, error) {

	reader, err := NewReader(src)
	if err != nil {
		return nil, fmt.Errorf("open source: %w", err)
	}

	files := reader.Files()
	totalLines := reader.TotalLines()

	var (
		scanned int64
		matches int64
		counts  []GrepFileCount
	)

	for _, f := range files {
		if filter != nil && !f.Orphan && f.Index != nil && filter.SkipFile(f.Index) {
			continue
		}

		fileMatches, n, err := grepFile(f, filter, cfg, onMatch)
		if err != nil {
			return counts, fmt.Errorf("grep %s: %w", f.Name, err)
		}

		scanned += n
		matches += fileMatches

		if fileMatches > 0 {
			counts = append(counts, GrepFileCount{File: f.Name, Count: fileMatches})
		}

		if progress != nil {
			progress(GrepProgress{Scanned: scanned, Total: totalLines, Matches: matches})
		}
	}

	return counts, nil
}

func grepFile(f FileInfo, filter *Filter, cfg GrepConfig, onMatch func(GrepMatch)) (int64, int64, error) {
	file, err := os.Open(f.Path)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = file.Close() }()

	var r io.Reader = file
	if strings.HasSuffix(f.Name, ".zst") {
		dec, err := zstd.NewReader(file)
		if err != nil {
			return 0, 0, fmt.Errorf("zstd open: %w", err)
		}
		defer dec.Close()
		r = dec
	}

	var scanned, matches int64
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry recv.LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		scanned++

		if filter != nil && !filter.MatchEntry(entry) {
			continue
		}

		matches++
		if !cfg.CountOnly && onMatch != nil {
			onMatch(GrepMatch{File: f.Name, Entry: entry})
		}
	}

	return matches, scanned, scanner.Err()
}
