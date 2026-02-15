package rotate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Config controls rotation behavior.
type Config struct {
	Dir      string // output directory
	MaxFile  int64  // max bytes per file before rotation
	MaxDisk  int64  // max total bytes on disk
	Compress bool   // zstd compress rotated files
}

// IndexEntry records metadata for one rotated file.
type IndexEntry struct {
	File   string                      `json:"file"`
	From   time.Time                   `json:"from"`
	To     time.Time                   `json:"to"`
	Lines  int64                       `json:"lines"`
	Bytes  int64                       `json:"bytes"`
	Labels map[string]map[string]int64 `json:"labels,omitempty"`
}

// Rotator manages the active log file, rotation, compression, and disk cap.
type Rotator struct {
	cfg Config

	mu         sync.Mutex
	active     *os.File
	activeSize int64
	activeName string
	diskUsage  int64
	seq        int // sequence within same second
	lastSecond string

	// tracking for current file's index entry
	from   time.Time
	to     time.Time
	lines  int64
	labels map[string]map[string]int64

	// optional callbacks for metrics
	onRotate func(reason string) // called on successful rotation
	onError  func()              // called on rotation error
}

// New creates a Rotator, scanning any existing files for disk usage.
func New(cfg Config) (*Rotator, error) {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}
	r := &Rotator{
		cfg:    cfg,
		labels: make(map[string]map[string]int64),
	}
	if err := r.bootstrap(); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if err := r.openNew(); err != nil {
		return nil, fmt.Errorf("open initial file: %w", err)
	}
	return r, nil
}

// SetOnRotate sets a callback invoked on each successful rotation with the reason.
func (r *Rotator) SetOnRotate(fn func(reason string)) {
	r.onRotate = fn
}

// SetOnError sets a callback invoked on each rotation error.
func (r *Rotator) SetOnError(fn func()) {
	r.onError = fn
}

// Write appends data to the active file, rotating if over MaxFile.
func (r *Rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.activeSize+int64(len(p)) > r.cfg.MaxFile && r.activeSize > 0 {
		if err := r.rotate(); err != nil {
			if r.onError != nil {
				r.onError()
			}
			return 0, fmt.Errorf("rotate: %w", err)
		}
		if r.onRotate != nil {
			r.onRotate("size")
		}
	}
	n, err := r.active.Write(p)
	r.activeSize += int64(n)
	r.diskUsage += int64(n)
	return n, err
}

// TrackLine accumulates metadata for the current file's index entry.
func (r *Rotator) TrackLine(ts time.Time, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines++
	if r.from.IsZero() || ts.Before(r.from) {
		r.from = ts
	}
	if ts.After(r.to) {
		r.to = ts
	}
	for k, v := range labels {
		if r.labels[k] == nil {
			r.labels[k] = make(map[string]int64)
		}
		r.labels[k][v]++
	}
}

// DiskUsage returns current total bytes on disk.
func (r *Rotator) DiskUsage() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.diskUsage
}

// Close flushes the active file and writes a final index entry.
func (r *Rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.active == nil {
		return nil
	}
	if err := r.active.Close(); err != nil {
		return err
	}

	// write index entry for final file if it has data
	if r.lines > 0 {
		entry := r.buildIndexEntry()
		if r.cfg.Compress {
			compressed, err := r.compressFile(r.activeName)
			if err != nil {
				return fmt.Errorf("compress final: %w", err)
			}
			entry.File = filepath.Base(compressed)
		}
		if err := r.appendIndex(entry); err != nil {
			return fmt.Errorf("write final index: %w", err)
		}
	}
	r.active = nil
	return nil
}

func (r *Rotator) bootstrap() error {
	entries, err := os.ReadDir(r.cfg.Dir)
	if err != nil {
		return err
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	r.diskUsage = total
	return nil
}

func (r *Rotator) openNew() error {
	name := r.nextFilename()
	f, err := os.Create(filepath.Join(r.cfg.Dir, name))
	if err != nil {
		return err
	}
	r.active = f
	r.activeName = name
	r.activeSize = 0
	r.from = time.Time{}
	r.to = time.Time{}
	r.lines = 0
	r.labels = make(map[string]map[string]int64)
	return nil
}

func (r *Rotator) nextFilename() string {
	now := time.Now().UTC()
	sec := now.Format("2006-01-02T150405")
	if sec == r.lastSecond {
		r.seq++
	} else {
		r.lastSecond = sec
		r.seq = 0
	}
	return fmt.Sprintf("%s-%03d.jsonl", sec, r.seq)
}

func (r *Rotator) rotate() error {
	if err := r.active.Close(); err != nil {
		return err
	}

	entry := r.buildIndexEntry()

	if r.cfg.Compress {
		compressed, err := r.compressFile(r.activeName)
		if err != nil {
			return fmt.Errorf("compress: %w", err)
		}
		// update disk usage: remove raw size, add compressed size
		info, err := os.Stat(compressed)
		if err != nil {
			return err
		}
		r.diskUsage = r.diskUsage - r.activeSize + info.Size()
		entry.File = filepath.Base(compressed)
	}

	if err := r.appendIndex(entry); err != nil {
		return err
	}

	if err := r.enforceDiskCap(); err != nil {
		return fmt.Errorf("enforce disk cap: %w", err)
	}

	return r.openNew()
}

func (r *Rotator) buildIndexEntry() IndexEntry {
	entry := IndexEntry{
		File:  r.activeName,
		From:  r.from,
		To:    r.to,
		Lines: r.lines,
		Bytes: r.activeSize,
	}
	if len(r.labels) > 0 {
		entry.Labels = r.labels
	}
	return entry
}

func (r *Rotator) compressFile(name string) (string, error) {
	srcPath := filepath.Join(r.cfg.Dir, name)
	dstPath := srcPath + ".zst"

	src, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}

	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return "", err
	}
	compressed := enc.EncodeAll(src, nil)
	if err := enc.Close(); err != nil {
		return "", err
	}

	if err := os.WriteFile(dstPath, compressed, 0o644); err != nil {
		return "", err
	}
	if err := os.Remove(srcPath); err != nil {
		return "", err
	}
	return dstPath, nil
}

func (r *Rotator) appendIndex(entry IndexEntry) error {
	f, err := os.OpenFile(filepath.Join(r.cfg.Dir, "index.jsonl"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", data)
	return err
}

func (r *Rotator) enforceDiskCap() error {
	if r.cfg.MaxDisk <= 0 {
		return nil
	}

	// recalculate disk usage from actual files
	r.diskUsage = 0
	entries, err := os.ReadDir(r.cfg.Dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		r.diskUsage += info.Size()
	}

	if r.diskUsage <= r.cfg.MaxDisk {
		return nil
	}

	// collect data files sorted by name (oldest first)
	var dataFiles []string
	for _, e := range entries {
		name := e.Name()
		if name == "index.jsonl" || name == "metadata.json" {
			continue
		}
		if strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".jsonl.zst") {
			dataFiles = append(dataFiles, name)
		}
	}
	sort.Strings(dataFiles)

	// track which files we delete so we can prune index
	deleted := make(map[string]bool)
	for _, name := range dataFiles {
		if r.diskUsage <= r.cfg.MaxDisk {
			break
		}
		path := filepath.Join(r.cfg.Dir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		size := info.Size()
		if err := os.Remove(path); err != nil {
			continue
		}
		r.diskUsage -= size
		deleted[name] = true
	}

	if len(deleted) > 0 {
		return r.pruneIndex(deleted)
	}
	return nil
}

func (r *Rotator) pruneIndex(deleted map[string]bool) error {
	indexPath := filepath.Join(r.cfg.Dir, "index.jsonl")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var kept [][]byte
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry IndexEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if !deleted[entry.File] {
			kept = append(kept, []byte(line))
		}
	}

	var out []byte
	for _, line := range kept {
		out = append(out, line...)
		out = append(out, '\n')
	}
	return os.WriteFile(indexPath, out, 0o644)
}
