package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

// GCOptions configures garbage collection of capture directories.
type GCOptions struct {
	MaxAge        time.Duration
	MaxTotalBytes int64
	DryRun        bool
	Now           time.Time
}

// GCDeletion records a capture directory selected for deletion.
type GCDeletion struct {
	Dir       string    `json:"dir"`
	Started   time.Time `json:"started"`
	SizeBytes int64     `json:"size_bytes"`
	Reasons   []string  `json:"reasons,omitempty"`
}

// GCResult summarizes a GC run.
type GCResult struct {
	Root          string        `json:"root"`
	CaptureCount  int           `json:"capture_count"`
	TotalBytes    int64         `json:"total_bytes"`
	MaxAge        time.Duration `json:"max_age,omitempty"`
	MaxTotalBytes int64         `json:"max_total_bytes,omitempty"`
	DryRun        bool          `json:"dry_run"`
	Deletions     []GCDeletion  `json:"deletions"`

	now time.Time
}

type captureInfo struct {
	Dir       string
	Started   time.Time
	SizeBytes int64
}

// GC scans subdirectories of root and deletes old or oversized captures.
func GC(root string, opts GCOptions) (*GCResult, error) {
	if opts.MaxAge <= 0 && opts.MaxTotalBytes <= 0 {
		return nil, fmt.Errorf("gc requires --max-age or --max-total")
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	captures, err := scanCaptures(root)
	if err != nil {
		return nil, err
	}

	result := &GCResult{
		Root:          root,
		CaptureCount:  len(captures),
		MaxAge:        opts.MaxAge,
		MaxTotalBytes: opts.MaxTotalBytes,
		DryRun:        opts.DryRun,
		now:           now,
	}

	for _, c := range captures {
		result.TotalBytes += c.SizeBytes
	}

	if len(captures) == 0 {
		return result, nil
	}

	deletions := make(map[string]*GCDeletion)
	mark := func(c captureInfo, reason string) {
		d, ok := deletions[c.Dir]
		if !ok {
			d = &GCDeletion{Dir: c.Dir, Started: c.Started, SizeBytes: c.SizeBytes}
			deletions[c.Dir] = d
		}
		for _, r := range d.Reasons {
			if r == reason {
				return
			}
		}
		d.Reasons = append(d.Reasons, reason)
	}

	if opts.MaxAge > 0 {
		cutoff := now.Add(-opts.MaxAge)
		for _, c := range captures {
			if c.Started.Before(cutoff) {
				mark(c, "max-age")
			}
		}
	}

	if opts.MaxTotalBytes > 0 {
		total := result.TotalBytes
		for _, d := range deletions {
			total -= d.SizeBytes
		}
		if total > opts.MaxTotalBytes {
			candidates := make([]captureInfo, 0, len(captures))
			for _, c := range captures {
				if deletions[c.Dir] != nil {
					continue
				}
				candidates = append(candidates, c)
			}
			sort.Slice(candidates, func(i, j int) bool {
				if candidates[i].Started.Equal(candidates[j].Started) {
					return candidates[i].Dir < candidates[j].Dir
				}
				return candidates[i].Started.Before(candidates[j].Started)
			})
			for _, c := range candidates {
				if total <= opts.MaxTotalBytes {
					break
				}
				mark(c, "max-total")
				total -= c.SizeBytes
			}
		}
	}

	if len(deletions) > 0 {
		result.Deletions = make([]GCDeletion, 0, len(deletions))
		for _, d := range deletions {
			result.Deletions = append(result.Deletions, *d)
		}
		sort.Slice(result.Deletions, func(i, j int) bool {
			if result.Deletions[i].Started.Equal(result.Deletions[j].Started) {
				return result.Deletions[i].Dir < result.Deletions[j].Dir
			}
			return result.Deletions[i].Started.Before(result.Deletions[j].Started)
		})
	}

	if opts.DryRun || len(result.Deletions) == 0 {
		return result, nil
	}

	for _, d := range result.Deletions {
		if _, err := os.Stat(filepath.Join(d.Dir, "metadata.json")); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return result, fmt.Errorf("check metadata: %w", err)
		}
		if err := os.RemoveAll(d.Dir); err != nil {
			return result, fmt.Errorf("delete %s: %w", d.Dir, err)
		}
	}

	return result, nil
}

// WriteJSON writes the deletion list as indented JSON.
func (r *GCResult) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.Deletions)
}

// WriteText writes a human-readable GC summary.
func (r *GCResult) WriteText(w io.Writer) {
	tw := &textWriter{w: w}

	tw.printf("Captures: %d   Total size: %s\n", r.CaptureCount, FormatBytes(r.TotalBytes))
	if r.DryRun {
		tw.printf("Dry run: would delete %d capture(s)\n", len(r.Deletions))
	} else {
		tw.printf("Deleted %d capture(s)\n", len(r.Deletions))
	}
	if len(r.Deletions) == 0 {
		return
	}
	for _, d := range r.Deletions {
		started := "unknown"
		if !d.Started.IsZero() {
			started = d.Started.Format("2006-01-02 15:04:05")
		}
		age := ""
		if !d.Started.IsZero() && !r.now.IsZero() {
			delta := r.now.Sub(d.Started)
			if delta > 0 {
				age = formatHumanDuration(delta)
			}
		}
		reason := ""
		if len(d.Reasons) > 0 {
			reason = strings.Join(d.Reasons, ", ")
		}

		line := fmt.Sprintf("  %s  %s  started %s", d.Dir, FormatBytes(d.SizeBytes), started)
		if age != "" {
			line += fmt.Sprintf("  age %s", age)
		}
		if reason != "" {
			line += fmt.Sprintf("  (%s)", reason)
		}
		tw.println(line)
	}
}

func scanCaptures(root string) ([]captureInfo, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var captures []captureInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		metaPath := filepath.Join(dir, "metadata.json")
		if _, err := os.Stat(metaPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat metadata: %w", err)
		}
		meta, err := recv.ReadMetadata(dir)
		if err != nil {
			return nil, fmt.Errorf("read metadata %s: %w", dir, err)
		}
		sizeBytes, err := dirSize(dir)
		if err != nil {
			return nil, fmt.Errorf("size %s: %w", dir, err)
		}
		captures = append(captures, captureInfo{Dir: dir, Started: meta.Started, SizeBytes: sizeBytes})
	}
	return captures, nil
}

func dirSize(dir string) (int64, error) {
	var total int64
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, walkErr
}
