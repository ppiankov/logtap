package archive

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

// SliceConfig controls slice output.
type SliceConfig struct {
	MaxFile  int64 // rotation size (default 1GB)
	Compress bool  // zstd compress (default true)
}

// SliceProgress reports progress during slicing.
type SliceProgress struct {
	Matched int64
	Total   int64 // source total lines from index
}

// Slice reads filtered entries from src and writes a new capture to dst.
func Slice(src, dst string, filter *Filter, cfg SliceConfig, progress func(SliceProgress)) error {
	reader, err := NewReader(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	srcMeta := reader.Metadata()
	totalLines := reader.TotalLines()

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	maxFile := cfg.MaxFile
	if maxFile <= 0 {
		maxFile = 1 << 30 // 1GB default
	}

	rot, err := rotate.New(rotate.Config{
		Dir:      dst,
		MaxFile:  maxFile,
		Compress: cfg.Compress,
	})
	if err != nil {
		return fmt.Errorf("create rotator: %w", err)
	}

	var matched int64
	labelsSeen := make(map[string]bool)

	_, err = reader.Scan(filter, func(e recv.LogEntry) bool {
		data, merr := json.Marshal(e)
		if merr != nil {
			return true
		}
		data = append(data, '\n')
		if _, werr := rot.Write(data); werr != nil {
			return true
		}
		rot.TrackLine(e.Timestamp, e.Labels)
		matched++

		for k := range e.Labels {
			labelsSeen[k] = true
		}

		if progress != nil && matched%10000 == 0 {
			progress(SliceProgress{
				Matched: matched,
				Total:   totalLines,
			})
		}
		return true
	})
	if err != nil {
		_ = rot.Close()
		return fmt.Errorf("scan source: %w", err)
	}

	if err := rot.Close(); err != nil {
		return fmt.Errorf("close rotator: %w", err)
	}

	// final progress
	if progress != nil {
		progress(SliceProgress{
			Matched: matched,
			Total:   totalLines,
		})
	}

	// write metadata for the sliced capture
	labels := make([]string, 0, len(labelsSeen))
	for k := range labelsSeen {
		labels = append(labels, k)
	}

	outMeta := &recv.Metadata{
		Version:    srcMeta.Version,
		Format:     srcMeta.Format,
		TotalLines: matched,
		TotalBytes: rot.DiskUsage(),
		LabelsSeen: labels,
		Redaction:  srcMeta.Redaction,
	}

	// set Started/Stopped from the output index
	index, ierr := readIndex(dst)
	if ierr == nil && len(index) > 0 {
		outMeta.Started = index[0].From
		outMeta.Stopped = index[len(index)-1].To
	}

	if err := recv.WriteMetadata(dst, outMeta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	return nil
}
