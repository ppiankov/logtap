package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

// Summary holds aggregated information about a capture directory.
type Summary struct {
	Dir         string                `json:"dir"`
	Meta        *recv.Metadata        `json:"metadata"`
	Files       int                   `json:"files"`
	TotalLines  int64                 `json:"total_lines"`
	TotalBytes  int64                 `json:"total_bytes"`
	DiskSize    int64                 `json:"disk_size"`
	DataFrom    time.Time             `json:"data_from,omitempty"`
	DataTo      time.Time             `json:"data_to,omitempty"`
	LinesPerSec float64               `json:"lines_per_sec,omitempty"`
	BucketWidth string                `json:"bucket_width,omitempty"`
	Labels      map[string][]LabelVal `json:"labels,omitempty"`
	Timeline    []Bucket              `json:"timeline,omitempty"`
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
	indexedFiles := make(map[string]bool, len(index))

	for _, entry := range index {
		indexedFiles[entry.File] = true
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

	// count lines from orphan files not yet in index
	orphans, oErr := discoverOrphans(dir, indexedFiles)
	if oErr == nil {
		for _, orph := range orphans {
			lines, bytes := countFileLines(orph.Path)
			s.TotalLines += lines
			s.TotalBytes += bytes
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

	// expose index time range
	s.DataFrom = minTime
	s.DataTo = maxTime

	// compute average rate
	start, stop := s.effectivePeriod()
	if !start.IsZero() && !stop.IsZero() && s.TotalLines > 0 {
		dur := stop.Sub(start).Seconds()
		if dur > 0 {
			s.LinesPerSec = float64(s.TotalLines) / dur
		}
	}

	// build timeline with adaptive bucket width
	if !minTime.IsZero() && !maxTime.IsZero() {
		bw, label := timelineBucketWidth(maxTime.Sub(minTime))
		s.BucketWidth = label
		s.Timeline = buildTimeline(index, minTime, maxTime, bw)
	}

	return s, nil
}

// effectivePeriod returns the best available start/stop times.
// Prefers metadata values, falls back to index data range.
func (s *Summary) effectivePeriod() (start, stop time.Time) {
	start = s.Meta.Started
	if start.IsZero() {
		start = s.DataFrom
	}
	stop = s.Meta.Stopped
	if stop.IsZero() {
		stop = s.DataTo
	}
	return start, stop
}

// timelineBucketWidth chooses an adaptive bucket width based on capture duration.
func timelineBucketWidth(d time.Duration) (width time.Duration, label string) {
	switch {
	case d < 2*time.Hour:
		return time.Minute, "1-min buckets"
	case d < 12*time.Hour:
		return 5 * time.Minute, "5-min buckets"
	case d < 3*24*time.Hour:
		return 15 * time.Minute, "15-min buckets"
	case d < 7*24*time.Hour:
		return time.Hour, "1-hour buckets"
	default:
		return 4 * time.Hour, "4-hour buckets"
	}
}

// buildTimeline distributes index entry lines across adaptive-width buckets.
func buildTimeline(index []rotate.IndexEntry, minTime, maxTime time.Time, bucketWidth time.Duration) []Bucket {
	if len(index) == 0 || minTime.IsZero() {
		return nil
	}

	start := minTime.Truncate(bucketWidth)
	end := maxTime.Truncate(bucketWidth)

	nBuckets := int(end.Sub(start)/bucketWidth) + 1
	if nBuckets <= 0 {
		nBuckets = 1
	}
	// safety cap
	if nBuckets > 10080 {
		nBuckets = 10080
	}

	buckets := make([]int64, nBuckets)

	for _, entry := range index {
		if entry.Lines == 0 {
			continue
		}
		entryStart := entry.From.Truncate(bucketWidth)
		entryEnd := entry.To.Truncate(bucketWidth)

		startIdx := int(entryStart.Sub(start) / bucketWidth)
		endIdx := int(entryEnd.Sub(start) / bucketWidth)

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
			Time:  start.Add(time.Duration(i) * bucketWidth),
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

// countFileLines counts non-empty lines and total bytes in a data file.
// Handles both plain .jsonl and compressed .jsonl.zst files.
func countFileLines(path string) (lines, bytes int64) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()

	var r io.Reader = f
	if strings.HasSuffix(path, ".zst") {
		dec, err := zstd.NewReader(f)
		if err != nil {
			return 0, 0
		}
		defer dec.Close()
		r = dec
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		lines++
		bytes += int64(len(line))
	}
	return lines, bytes
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

	// period — prefer metadata, fall back to index data range
	start, stop := s.effectivePeriod()
	if !start.IsZero() {
		startStr := start.Format("2006-01-02 15:04:05")
		if !stop.IsZero() && stop.After(start) {
			var stopStr string
			if start.YearDay() == stop.YearDay() && start.Year() == stop.Year() {
				stopStr = stop.Format("15:04:05")
			} else {
				stopStr = stop.Format("2006-01-02 15:04:05")
			}
			dur := stop.Sub(start)
			tw.printf("Period:  %s — %s (%s)\n", startStr, stopStr, formatHumanDuration(dur))
		} else {
			tw.printf("Period:  %s\n", startStr)
		}
	}

	tw.printf("Size:    %s (%d files)\n", FormatBytes(s.DiskSize), s.Files)

	// uncompressed data size (only when different from disk size)
	if s.TotalBytes > 0 && s.TotalBytes != s.DiskSize {
		tw.printf("Data:    %s (uncompressed)\n", FormatBytes(s.TotalBytes))
	}

	tw.printf("Lines:   %s\n", FormatCount(s.TotalLines))

	// average rate
	if s.LinesPerSec > 0 {
		tw.printf("Rate:    ~%s lines/sec\n", FormatCount(int64(s.LinesPerSec)))
	}

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
				if pct > 0 && pct < 0.05 {
					tw.printf("    %-20s %s lines   %s   (< 0.1%%)\n",
						v.Value, FormatCount(v.Lines), FormatBytes(v.Bytes))
				} else {
					tw.printf("    %-20s %s lines   %s   (%.1f%%)\n",
						v.Value, FormatCount(v.Lines), FormatBytes(v.Bytes), pct)
				}
			}
			tw.println()
		}
	}

	// timeline
	if len(s.Timeline) > 0 {
		label := s.BucketWidth
		if label == "" {
			label = "1-min buckets"
		}
		tw.printf("Timeline (%s):\n", label)
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

// FormatBytes formats byte counts in human-readable form.
func FormatBytes(b int64) string {
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

// FormatCount formats large numbers with comma separators.
func FormatCount(n int64) string {
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

// formatHumanDuration formats a duration as "Xd Yh", "Xh Ym", or "Xm Ys".
func formatHumanDuration(d time.Duration) string {
	totalH := int(d.Hours())
	days := totalH / 24
	h := totalH % 24
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %02dh", days, h)
	}
	if totalH > 0 {
		return fmt.Sprintf("%dh %02dm", totalH, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
