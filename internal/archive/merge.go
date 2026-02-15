package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

// MergeProgress reports progress during merge.
type MergeProgress struct {
	FilesCopied int
	TotalFiles  int
}

// Merge combines multiple capture directories into one by copying compressed
// files without decompressing. The output index.jsonl is sorted by timestamp.
func Merge(sources []string, dst string, progress func(MergeProgress)) error {
	if len(sources) < 2 {
		return fmt.Errorf("merge requires at least 2 source captures")
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// collect files and metadata from all sources
	var (
		allFiles   []mergeFile
		allMeta    []*recv.Metadata
		totalFiles int
	)

	for _, src := range sources {
		reader, err := NewReader(src)
		if err != nil {
			return fmt.Errorf("open %s: %w", src, err)
		}
		allMeta = append(allMeta, reader.Metadata())

		for _, f := range reader.Files() {
			allFiles = append(allFiles, mergeFile{info: f, srcDir: src})
		}
		totalFiles += len(reader.Files())
	}

	// resolve name collisions and copy files
	usedNames := make(map[string]bool)
	var mergedIndex []rotate.IndexEntry
	copied := 0

	for i := range allFiles {
		mf := &allFiles[i]

		// resolve collision
		dstName := mf.info.Name
		if usedNames[dstName] {
			dstName = resolveCollision(dstName, usedNames)
		}
		usedNames[dstName] = true

		// copy file
		if err := copyFile(mf.info.Path, filepath.Join(dst, dstName)); err != nil {
			return fmt.Errorf("copy %s: %w", mf.info.Name, err)
		}

		// update index entry with new filename
		if mf.info.Index != nil {
			entry := *mf.info.Index
			entry.File = dstName
			mergedIndex = append(mergedIndex, entry)
		}

		copied++
		if progress != nil {
			progress(MergeProgress{FilesCopied: copied, TotalFiles: totalFiles})
		}
	}

	// sort index by From timestamp
	sort.Slice(mergedIndex, func(i, j int) bool {
		return mergedIndex[i].From.Before(mergedIndex[j].From)
	})

	// write index.jsonl
	if err := writeIndexFile(dst, mergedIndex); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	// merge metadata
	meta := mergeMetadata(allMeta, mergedIndex)
	if err := recv.WriteMetadata(dst, meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	return nil
}

type mergeFile struct {
	info   FileInfo
	srcDir string
}

// resolveCollision generates a new filename that doesn't collide.
func resolveCollision(name string, used map[string]bool) string {
	ext := ".jsonl"
	if strings.HasSuffix(name, ".jsonl.zst") {
		ext = ".jsonl.zst"
	}
	base := strings.TrimSuffix(name, ext)

	// try incrementing sequence: base is like "2024-01-15T100000-000"
	// find the dash-NNN suffix and increment
	if idx := strings.LastIndex(base, "-"); idx >= 0 {
		prefix := base[:idx]
		for seq := 1; seq < 1000; seq++ {
			candidate := fmt.Sprintf("%s-%03d%s", prefix, seq, ext)
			if !used[candidate] {
				return candidate
			}
		}
	}

	// fallback: append suffix
	for seq := 1; seq < 1000; seq++ {
		candidate := fmt.Sprintf("%s-m%03d%s", base, seq, ext)
		if !used[candidate] {
			return candidate
		}
	}
	return name // should never happen
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func writeIndexFile(dir string, entries []rotate.IndexEntry) error {
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
	return f.Close()
}

func mergeMetadata(metas []*recv.Metadata, index []rotate.IndexEntry) *recv.Metadata {
	out := &recv.Metadata{
		Version: 1,
		Format:  "jsonl",
	}

	labelSet := make(map[string]bool)
	for _, m := range metas {
		out.TotalLines += m.TotalLines
		out.TotalBytes += m.TotalBytes

		if out.Started.IsZero() || (!m.Started.IsZero() && m.Started.Before(out.Started)) {
			out.Started = m.Started
		}
		if m.Stopped.After(out.Stopped) {
			out.Stopped = m.Stopped
		}

		for _, l := range m.LabelsSeen {
			labelSet[l] = true
		}

		if m.Redaction != nil && m.Redaction.Enabled {
			out.Redaction = m.Redaction
		}
	}

	// also collect labels from index entries
	for _, ie := range index {
		for k := range ie.Labels {
			labelSet[k] = true
		}
	}

	// override Started/Stopped from index if available
	if len(index) > 0 {
		first := index[0].From
		last := index[len(index)-1].To
		if !first.IsZero() {
			out.Started = first
		}
		if !last.IsZero() && last.After(out.Stopped) {
			out.Stopped = last
		}
	}

	out.LabelsSeen = make([]string, 0, len(labelSet))
	for k := range labelSet {
		out.LabelsSeen = append(out.LabelsSeen, k)
	}
	sort.Strings(out.LabelsSeen)

	return out
}
