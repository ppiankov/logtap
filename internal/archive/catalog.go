package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

// CatalogEntry represents one discovered capture directory.
type CatalogEntry struct {
	Dir     string    `json:"dir"`
	Started time.Time `json:"started"`
	Stopped time.Time `json:"stopped,omitempty"`
	Files   int       `json:"files"`
	Entries int64     `json:"entries"`
	Bytes   int64     `json:"bytes"`
	Active  bool      `json:"active"`
	Labels  []string  `json:"labels,omitempty"`
}

// Catalog scans root for capture directories containing metadata.json.
// If recursive is true, it walks subdirectories.
func Catalog(root string, recursive bool) ([]CatalogEntry, error) {
	var entries []CatalogEntry

	if recursive {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // skip inaccessible dirs
			}
			if d.IsDir() && path != root {
				if e, ok := tryCapture(path); ok {
					entries = append(entries, e)
					return filepath.SkipDir // don't recurse into capture dirs
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", root, err)
		}
	} else {
		dirEntries, err := os.ReadDir(root)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", root, err)
		}
		for _, d := range dirEntries {
			if !d.IsDir() {
				continue
			}
			path := filepath.Join(root, d.Name())
			if e, ok := tryCapture(path); ok {
				entries = append(entries, e)
			}
		}
	}

	// Also check if root itself is a capture
	if e, ok := tryCapture(root); ok {
		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Started.After(entries[j].Started)
	})

	return entries, nil
}

func tryCapture(dir string) (CatalogEntry, bool) {
	meta, err := recv.ReadMetadata(dir)
	if err != nil {
		return CatalogEntry{}, false
	}

	diskSize, fileCount := diskStats(dir)

	return CatalogEntry{
		Dir:     dir,
		Started: meta.Started,
		Stopped: meta.Stopped,
		Files:   fileCount,
		Entries: meta.TotalLines,
		Bytes:   diskSize,
		Active:  meta.Stopped.IsZero(),
		Labels:  meta.LabelsSeen,
	}, true
}

// WriteCatalogJSON writes catalog entries as JSON.
func WriteCatalogJSON(w io.Writer, entries []CatalogEntry) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

// WriteCatalogText writes catalog entries as a formatted table.
func WriteCatalogText(w io.Writer, entries []CatalogEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(w, "No captures found.")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CAPTURE\tSTARTED\tSTOPPED\tFILES\tENTRIES\tSIZE")
	for _, e := range entries {
		started := e.Started.Format("2006-01-02 15:04")
		stopped := "(active)"
		if !e.Active {
			stopped = e.Stopped.Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n",
			e.Dir, started, stopped, e.Files, FormatCount(e.Entries), formatBytes(e.Bytes))
	}
	tw.Flush()
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
