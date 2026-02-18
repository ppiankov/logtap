package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

// FileInfo describes a data file in a capture directory.
type FileInfo struct {
	Path   string
	Name   string
	Index  *rotate.IndexEntry // nil for orphan files
	Orphan bool
}

// Reader provides streaming access to a capture directory.
type Reader struct {
	dir   string
	meta  *recv.Metadata
	files []FileInfo
}

// NewReader opens a capture directory and resolves its file list.
func NewReader(dir string) (*Reader, error) {
	meta, err := recv.ReadMetadata(dir)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	index, err := readIndex(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read index: %w", err)
	}

	// build file list from index
	indexedFiles := make(map[string]bool, len(index))
	var files []FileInfo
	for i := range index {
		entry := &index[i]
		indexedFiles[entry.File] = true
		files = append(files, FileInfo{
			Path:  filepath.Join(dir, entry.File),
			Name:  entry.File,
			Index: entry,
		})
	}

	// discover orphans
	orphans, err := discoverOrphans(dir, indexedFiles)
	if err != nil {
		return nil, fmt.Errorf("discover orphans: %w", err)
	}
	files = append(files, orphans...)

	// sort by filename for chronological order
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})

	return &Reader{dir: dir, meta: meta, files: files}, nil
}

// Metadata returns the capture session metadata.
func (r *Reader) Metadata() *recv.Metadata {
	return r.meta
}

// Files returns the resolved file list for parallel scanning.
func (r *Reader) Files() []FileInfo {
	return r.files
}

// TotalLines returns the sum of lines from all files, including orphans.
func (r *Reader) TotalLines() int64 {
	var total int64
	for _, f := range r.files {
		if f.Index != nil {
			total += f.Index.Lines
		} else if f.Orphan {
			lines, _ := countFileLines(f.Path)
			total += lines
		}
	}
	return total
}

// Scan iterates all files in order, applying the filter and calling fn for each matching entry.
// If fn returns false, scanning stops early. Returns total lines scanned and any error.
func (r *Reader) Scan(filter *Filter, fn func(recv.LogEntry) bool) (int64, error) {
	var scanned int64
	for _, f := range r.files {
		if filter != nil && !f.Orphan && f.Index != nil && filter.SkipFile(f.Index) {
			continue
		}

		n, stop, err := r.scanFile(f, filter, fn)
		scanned += n
		if err != nil {
			return scanned, fmt.Errorf("scan %s: %w", f.Name, err)
		}
		if stop {
			break
		}
	}
	return scanned, nil
}

func (r *Reader) scanFile(f FileInfo, filter *Filter, fn func(recv.LogEntry) bool) (int64, bool, error) {
	file, err := os.Open(f.Path)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = file.Close() }()

	var reader io.Reader = file
	if strings.HasSuffix(f.Name, ".zst") {
		dec, err := zstd.NewReader(file)
		if err != nil {
			return 0, false, fmt.Errorf("zstd open: %w", err)
		}
		defer dec.Close()
		reader = dec
	}

	var scanned int64
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry recv.LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed lines
		}
		scanned++

		if filter != nil && !filter.MatchEntry(entry) {
			continue
		}
		if !fn(entry) {
			return scanned, true, nil
		}
	}
	return scanned, false, scanner.Err()
}

func readIndex(dir string) ([]rotate.IndexEntry, error) {
	data, err := os.ReadFile(filepath.Join(dir, "index.jsonl"))
	if err != nil {
		return nil, err
	}

	var entries []rotate.IndexEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry rotate.IndexEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func discoverOrphans(dir string, indexed map[string]bool) ([]FileInfo, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var orphans []FileInfo
	for _, e := range dirEntries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if name == "index.jsonl" || name == "metadata.json" || name == ".gitkeep" {
			continue
		}
		if !strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".jsonl.zst") {
			continue
		}
		if indexed[name] {
			continue
		}
		orphans = append(orphans, FileInfo{
			Path:   filepath.Join(dir, name),
			Name:   name,
			Orphan: true,
		})
	}
	return orphans, nil
}
