package recv

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Tailer follows the active JSONL file in a capture directory, emitting new
// lines as they are appended. It handles file rotation by switching to the
// newest uncompressed .jsonl file when a new one appears.
type Tailer struct {
	dir     string
	file    *os.File
	reader  *bufio.Reader
	current string // current filename
}

// NewTailer opens the newest .jsonl file in dir and seeks to the end.
// Use Tail() in a loop to read new entries.
func NewTailer(dir string) (*Tailer, error) {
	name, err := newestJSONL(dir)
	if err != nil {
		return nil, err
	}

	t := &Tailer{dir: dir}
	if err := t.openFile(name, true); err != nil {
		return nil, err
	}
	return t, nil
}

// NewTailerFromStart opens the newest .jsonl file and reads from the beginning.
func NewTailerFromStart(dir string) (*Tailer, error) {
	name, err := newestJSONL(dir)
	if err != nil {
		return nil, err
	}

	t := &Tailer{dir: dir}
	if err := t.openFile(name, false); err != nil {
		return nil, err
	}
	return t, nil
}

// Tail reads any new complete lines from the current file. On rotation
// (new .jsonl file detected), it switches to the new file. Returns entries
// read and any error. Returns nil, nil when no new data is available.
func (t *Tailer) Tail() ([]LogEntry, error) {
	// Check for rotation
	newest, err := newestJSONL(t.dir)
	if err == nil && newest != t.current {
		_ = t.file.Close()
		if err := t.openFile(newest, false); err != nil {
			return nil, err
		}
	}

	var entries []LogEntry
	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return entries, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ReadLast reads the last n lines from the current file.
func (t *Tailer) ReadLast(n int) ([]LogEntry, error) {
	// Seek to beginning and read all
	if _, err := t.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	t.reader.Reset(t.file)

	var all []LogEntry
	scanner := bufio.NewScanner(t.file)
	scanner.Buffer(make([]byte, 256<<10), 256<<10)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		all = append(all, entry)
	}

	// Seek back to end for subsequent Tail calls
	if _, err := t.file.Seek(0, io.SeekEnd); err != nil {
		return nil, err
	}
	t.reader.Reset(t.file)

	if n >= len(all) {
		return all, nil
	}
	return all[len(all)-n:], nil
}

// Close closes the underlying file.
func (t *Tailer) Close() error {
	if t.file != nil {
		return t.file.Close()
	}
	return nil
}

func (t *Tailer) openFile(name string, seekEnd bool) error {
	path := filepath.Join(t.dir, name)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	if seekEnd {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			_ = f.Close()
			return err
		}
	}
	t.file = f
	t.reader = bufio.NewReader(f)
	t.current = name
	return nil
}

// newestJSONL finds the most recently modified .jsonl file (not .zst) in dir.
func newestJSONL(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var jsonlFiles []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".zst") && name != "index.jsonl" && name != "audit.jsonl" {
			jsonlFiles = append(jsonlFiles, e)
		}
	}

	if len(jsonlFiles) == 0 {
		return "", os.ErrNotExist
	}

	sort.Slice(jsonlFiles, func(i, j int) bool {
		fi, _ := jsonlFiles[i].Info()
		fj, _ := jsonlFiles[j].Info()
		if fi == nil || fj == nil {
			return jsonlFiles[i].Name() > jsonlFiles[j].Name()
		}
		return fi.ModTime().After(fj.ModTime())
	})

	return jsonlFiles[0].Name(), nil
}

// IsLiveCapture checks if a capture directory is still actively receiving.
func IsLiveCapture(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return false
	}
	var meta struct {
		Stopped time.Time `json:"stopped"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return false
	}
	return meta.Stopped.IsZero()
}
