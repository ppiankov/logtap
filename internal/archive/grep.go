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
	Context   int  // number of surrounding lines to include (0 = matches only)
}

// GrepMatch represents a matching entry with file context.
type GrepMatch struct {
	File    string
	Entry   recv.LogEntry
	Context string // "" for actual match, "before" or "after" for context lines
	Group   int    // context group ID (0 when context is not used); entries in the same group are contiguous
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

	// When context is requested, collect all entries and match indices,
	// then expand ranges and emit with context markers.
	if cfg.Context > 0 && !cfg.CountOnly && onMatch != nil {
		return grepFileWithContext(f.Name, r, filter, cfg.Context, onMatch)
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

// grepFileWithContext scans a file, collecting all entries and tracking match
// positions, then emits matches with surrounding context lines.
func grepFileWithContext(name string, r io.Reader, filter *Filter, ctx int,
	onMatch func(GrepMatch)) (int64, int64, error) {

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)

	var entries []recv.LogEntry
	var matchIndices []int

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry recv.LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		idx := len(entries)
		entries = append(entries, entry)
		if filter == nil || filter.MatchEntry(entry) {
			matchIndices = append(matchIndices, idx)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}

	scanned := int64(len(entries))
	matches := int64(len(matchIndices))

	if matches == 0 {
		return 0, scanned, nil
	}

	// Build a set of match indices for O(1) lookup.
	matchSet := make(map[int]struct{}, len(matchIndices))
	for _, i := range matchIndices {
		matchSet[i] = struct{}{}
	}

	// Merge overlapping context ranges.
	type span struct{ lo, hi int }
	var spans []span
	n := len(entries)
	for _, mi := range matchIndices {
		lo := mi - ctx
		if lo < 0 {
			lo = 0
		}
		hi := mi + ctx
		if hi >= n {
			hi = n - 1
		}
		if len(spans) > 0 && lo <= spans[len(spans)-1].hi+1 {
			// Extend the previous span.
			if hi > spans[len(spans)-1].hi {
				spans[len(spans)-1].hi = hi
			}
		} else {
			spans = append(spans, span{lo, hi})
		}
	}

	// Emit entries within merged spans.
	for spanIdx, s := range spans {
		for i := s.lo; i <= s.hi; i++ {
			ctxLabel := ""
			if _, isMatch := matchSet[i]; !isMatch {
				// Determine if this is before or after the nearest match.
				ctxLabel = "after" // default
				for _, mi := range matchIndices {
					if mi > i {
						ctxLabel = "before"
						break
					}
					if mi == i {
						break
					}
				}
			}
			onMatch(GrepMatch{File: name, Entry: entries[i], Context: ctxLabel, Group: spanIdx + 1})
		}
	}

	return matches, scanned, nil
}
