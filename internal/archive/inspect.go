package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

// Summary holds aggregated information about a capture directory.
type Summary struct {
	Dir        string                `json:"dir"`
	Meta       *recv.Metadata        `json:"metadata"`
	Files      int                   `json:"files"`
	TotalLines int64                 `json:"total_lines"`
	TotalBytes int64                 `json:"total_bytes"`
	DiskSize   int64                 `json:"disk_size"`
	Labels     map[string][]LabelVal `json:"labels,omitempty"`
	Timeline   []Bucket              `json:"timeline,omitempty"`
}

// LabelVal summarizes one label value's contribution.
type LabelVal struct {
	Value string `json:"value"`
	Lines int64  `json:"lines"`
	Bytes int64  `json:"bytes"`
}

// Bucket represents a 1-minute timeline bucket.
type Bucket struct {
	Time  time.Time `json:"time"`
	Lines int64     `json:"lines"`
}

// Inspect reads metadata.json and index.jsonl from a capture directory
// and returns an aggregated summary. No decompression is performed.
func Inspect(dir string) (*Summary, error) {
	meta, err := recv.ReadMetadata(dir)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	index, err := readIndex(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read index: %w", err)
	}

	diskSize, fileCount := diskStats(dir)

	s := &Summary{
		Dir:      dir,
		Meta:     meta,
		Files:    fileCount,
		DiskSize: diskSize,
	}

	// aggregate from index entries
	labelAcc := make(map[string]map[string]*LabelVal) // key -> value -> accumulator
	var minTime, maxTime time.Time

	for _, entry := range index {
		s.TotalLines += entry.Lines
		s.TotalBytes += entry.Bytes

		if minTime.IsZero() || entry.From.Before(minTime) {
			minTime = entry.From
		}
		if entry.To.After(maxTime) {
			maxTime = entry.To
		}

		// accumulate labels
		for key, vals := range entry.Labels {
			if labelAcc[key] == nil {
				labelAcc[key] = make(map[string]*LabelVal)
			}
			for val, count := range vals {
				if labelAcc[key][val] == nil {
					labelAcc[key][val] = &LabelVal{Value: val}
				}
				labelAcc[key][val].Lines += count
				// distribute bytes proportionally by line fraction
				if entry.Lines > 0 {
					labelAcc[key][val].Bytes += entry.Bytes * count / entry.Lines
				}
			}
		}
	}

	// convert label accumulators to sorted slices
	if len(labelAcc) > 0 {
		s.Labels = make(map[string][]LabelVal, len(labelAcc))
		for key, vals := range labelAcc {
			sorted := make([]LabelVal, 0, len(vals))
			for _, v := range vals {
				sorted = append(sorted, *v)
			}
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].Lines > sorted[j].Lines
			})
			s.Labels[key] = sorted
		}
	}

	// build timeline
	s.Timeline = buildTimeline(index, minTime, maxTime)

	return s, nil
}

// buildTimeline distributes index entry lines across 1-minute buckets.
func buildTimeline(index []rotate.IndexEntry, minTime, maxTime time.Time) []Bucket {
	if len(index) == 0 || minTime.IsZero() {
		return nil
	}

	// truncate to minute boundaries
	start := minTime.Truncate(time.Minute)
	end := maxTime.Truncate(time.Minute)

	nBuckets := int(end.Sub(start)/time.Minute) + 1
	if nBuckets <= 0 {
		nBuckets = 1
	}
	// safety cap — extremely long captures
	if nBuckets > 10080 { // 1 week of minutes
		nBuckets = 10080
	}

	buckets := make([]int64, nBuckets)

	for _, entry := range index {
		if entry.Lines == 0 {
			continue
		}
		entryStart := entry.From.Truncate(time.Minute)
		entryEnd := entry.To.Truncate(time.Minute)

		startIdx := int(entryStart.Sub(start) / time.Minute)
		endIdx := int(entryEnd.Sub(start) / time.Minute)

		if startIdx < 0 {
			startIdx = 0
		}
		if endIdx >= nBuckets {
			endIdx = nBuckets - 1
		}

		span := endIdx - startIdx + 1
		perBucket := entry.Lines / int64(span)
		remainder := entry.Lines % int64(span)

		for i := startIdx; i <= endIdx; i++ {
			buckets[i] += perBucket
			if int64(i-startIdx) < remainder {
				buckets[i]++
			}
		}
	}

	result := make([]Bucket, nBuckets)
	for i := range result {
		result[i] = Bucket{
			Time:  start.Add(time.Duration(i) * time.Minute),
			Lines: buckets[i],
		}
	}
	return result
}

// diskStats returns total size and count of data files in a capture directory.
func diskStats(dir string) (totalSize int64, fileCount int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "metadata.json" || name == "index.jsonl" || name == ".gitkeep" {
			continue
		}
		if !strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".jsonl.zst") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		totalSize += info.Size()
		fileCount++
	}
	return totalSize, fileCount
}

// textWriter wraps an io.Writer and captures the first error.
type textWriter struct {
	w   io.Writer
	err error
}

func (tw *textWriter) printf(format string, args ...any) {
	if tw.err != nil {
		return
	}
	_, tw.err = fmt.Fprintf(tw.w, format, args...)
}

func (tw *textWriter) println(args ...any) {
	if tw.err != nil {
		return
	}
	_, tw.err = fmt.Fprintln(tw.w, args...)
}

// WriteText renders the summary as human-readable text.
func (s *Summary) WriteText(w io.Writer) {
	tw := &textWriter{w: w}

	// header
	tw.printf("Capture: %s\n", s.Dir)
	tw.printf("Format:  %s (v%d)\n", s.Meta.Format, s.Meta.Version)

	if !s.Meta.Started.IsZero() {
		start := s.Meta.Started.Format("2006-01-02 15:04:05")
		if !s.Meta.Stopped.IsZero() {
			stop := s.Meta.Stopped.Format("15:04:05")
			dur := s.Meta.Stopped.Sub(s.Meta.Started)
			tw.printf("Period:  %s — %s (%s)\n", start, stop, formatHumanDuration(dur))
		} else {
			tw.printf("Period:  %s\n", start)
		}
	}

	tw.printf("Size:    %s (%d files)\n", formatBytes(s.DiskSize), s.Files)
	tw.printf("Lines:   %s\n", formatCount(s.TotalLines))

	// labels
	if len(s.Labels) > 0 {
		// sort label keys for deterministic output
		keys := make([]string, 0, len(s.Labels))
		for k := range s.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		tw.println()
		tw.println("Labels:")
		for _, key := range keys {
			vals := s.Labels[key]
			tw.printf("  %s:\n", key)
			for _, v := range vals {
				pct := float64(0)
				if s.TotalLines > 0 {
					pct = float64(v.Lines) / float64(s.TotalLines) * 100
				}
				tw.printf("    %-20s %s lines   %s   (%.1f%%)\n",
					v.Value, formatCount(v.Lines), formatBytes(v.Bytes), pct)
			}
			tw.println()
		}
	}

	// timeline
	if len(s.Timeline) > 0 {
		tw.println("Timeline (1-min buckets):")
		writeSparkline(tw, s.Timeline)
	}
}

// WriteJSON renders the summary as indented JSON.
func (s *Summary) WriteJSON(w io.Writer) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w)
	return err
}

var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// writeSparkline renders timeline buckets as a sparkline chart.
func writeSparkline(tw *textWriter, buckets []Bucket) {
	if len(buckets) == 0 {
		return
	}

	var maxLines int64
	for _, b := range buckets {
		if b.Lines > maxLines {
			maxLines = b.Lines
		}
	}

	charsPerRow := 30
	for i := 0; i < len(buckets); i += charsPerRow {
		end := i + charsPerRow
		if end > len(buckets) {
			end = len(buckets)
		}

		label := buckets[i].Time.Format("15:04")
		tw.printf("  %s ", label)

		for j := i; j < end; j++ {
			if maxLines == 0 {
				tw.printf("%s", string(sparkBlocks[0]))
				continue
			}
			ratio := float64(buckets[j].Lines) / float64(maxLines)
			idx := int(math.Round(ratio * float64(len(sparkBlocks)-1)))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(sparkBlocks) {
				idx = len(sparkBlocks) - 1
			}
			tw.printf("%s", string(sparkBlocks[idx]))
		}
		tw.println()
	}
}

// formatBytes formats byte counts in human-readable form.
func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatCount formats large numbers with comma separators.
func formatCount(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}

// formatHumanDuration formats a duration as "Xh Ym" or "Xm Ys".
func formatHumanDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
