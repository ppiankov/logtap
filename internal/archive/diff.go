package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

// DiffResult holds the comparison between two captures.
type DiffResult struct {
	A DiffCapture `json:"a"`
	B DiffCapture `json:"b"`

	LabelsOnlyA []string       `json:"labels_only_a,omitempty"`
	LabelsOnlyB []string       `json:"labels_only_b,omitempty"`
	ErrorsOnlyA []ErrorSummary `json:"errors_only_a,omitempty"`
	ErrorsOnlyB []ErrorSummary `json:"errors_only_b,omitempty"`
	RateCompare []RateBucket   `json:"rate_compare,omitempty"`
}

// DiffCapture summarizes one side of the comparison.
type DiffCapture struct {
	Dir       string        `json:"dir"`
	Started   time.Time     `json:"started"`
	Stopped   time.Time     `json:"stopped"`
	Duration  time.Duration `json:"duration"`
	Lines     int64         `json:"lines"`
	LinesPerS float64       `json:"lines_per_sec"`
	Labels    []string      `json:"labels"`
}

// ErrorSummary is a simplified error pattern with count.
type ErrorSummary struct {
	Pattern string `json:"pattern"`
	Count   int64  `json:"count"`
}

// RateBucket compares log rates at a given minute.
type RateBucket struct {
	Minute time.Time `json:"minute"`
	RateA  int64     `json:"rate_a"`
	RateB  int64     `json:"rate_b"`
}

// Diff compares two capture directories.
func Diff(srcA, srcB string) (*DiffResult, error) {
	capA, err := summarizeCapture(srcA)
	if err != nil {
		return nil, fmt.Errorf("capture A: %w", err)
	}
	capB, err := summarizeCapture(srcB)
	if err != nil {
		return nil, fmt.Errorf("capture B: %w", err)
	}

	result := &DiffResult{A: capA.summary, B: capB.summary}

	// Label diff
	aLabels := setFromSlice(capA.summary.Labels)
	bLabels := setFromSlice(capB.summary.Labels)
	for _, l := range capA.summary.Labels {
		if !bLabels[l] {
			result.LabelsOnlyA = append(result.LabelsOnlyA, l)
		}
	}
	for _, l := range capB.summary.Labels {
		if !aLabels[l] {
			result.LabelsOnlyB = append(result.LabelsOnlyB, l)
		}
	}

	// Error pattern diff
	aErrors := make(map[string]int64)
	for _, e := range capA.errors {
		aErrors[e.Pattern] = e.Count
	}
	bErrors := make(map[string]int64)
	for _, e := range capB.errors {
		bErrors[e.Pattern] = e.Count
	}
	for _, e := range capA.errors {
		if _, ok := bErrors[e.Pattern]; !ok {
			result.ErrorsOnlyA = append(result.ErrorsOnlyA, e)
		}
	}
	for _, e := range capB.errors {
		if _, ok := aErrors[e.Pattern]; !ok {
			result.ErrorsOnlyB = append(result.ErrorsOnlyB, e)
		}
	}

	// Rate comparison (per-minute buckets, aligned to earlier start)
	result.RateCompare = buildRateComparison(capA.rates, capB.rates)

	return result, nil
}

// WriteJSON writes the diff result as JSON.
func (d *DiffResult) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

// WriteText writes a human-readable diff summary.
func (d *DiffResult) WriteText(w io.Writer) {
	tw := &textWriter{w: w}

	tw.printf("Capture A: %s\n", d.A.Dir)
	tw.printf("  %d lines, %.1f lines/sec, %s\n", d.A.Lines, d.A.LinesPerS, d.A.Duration)
	tw.printf("Capture B: %s\n", d.B.Dir)
	tw.printf("  %d lines, %.1f lines/sec, %s\n", d.B.Lines, d.B.LinesPerS, d.B.Duration)

	if len(d.LabelsOnlyA) > 0 {
		tw.printf("\nLabels only in A: %v\n", d.LabelsOnlyA)
	}
	if len(d.LabelsOnlyB) > 0 {
		tw.printf("\nLabels only in B: %v\n", d.LabelsOnlyB)
	}

	if len(d.ErrorsOnlyA) > 0 {
		tw.printf("\nErrors only in A:\n")
		for _, e := range d.ErrorsOnlyA {
			tw.printf("  [%d] %s\n", e.Count, e.Pattern)
		}
	}
	if len(d.ErrorsOnlyB) > 0 {
		tw.printf("\nErrors only in B:\n")
		for _, e := range d.ErrorsOnlyB {
			tw.printf("  [%d] %s\n", e.Count, e.Pattern)
		}
	}

	if len(d.RateCompare) > 0 {
		tw.printf("\nRate comparison (lines/min):\n")
		tw.printf("  %-20s %8s %8s\n", "Minute", "A", "B")
		for _, b := range d.RateCompare {
			tw.printf("  %-20s %8d %8d\n", b.Minute.Format("15:04"), b.RateA, b.RateB)
		}
	}
}

type captureData struct {
	summary DiffCapture
	errors  []ErrorSummary
	rates   map[time.Time]int64 // per-minute counts
}

func summarizeCapture(dir string) (*captureData, error) {
	r, err := NewReader(dir)
	if err != nil {
		return nil, err
	}

	meta := r.Metadata()
	duration := meta.Stopped.Sub(meta.Started)
	linesPerSec := float64(0)
	if duration > 0 {
		linesPerSec = float64(meta.TotalLines) / duration.Seconds()
	}

	// Collect labels from index
	labelSet := make(map[string]bool)
	for _, f := range r.Files() {
		if f.Index != nil {
			for k := range f.Index.Labels {
				labelSet[k] = true
			}
		}
	}
	labels := make([]string, 0, len(labelSet))
	for k := range labelSet {
		labels = append(labels, k)
	}
	sort.Strings(labels)

	// Scan for errors and per-minute rates
	errorCounts := make(map[string]int64)
	rates := make(map[time.Time]int64)

	_, err = r.Scan(nil, func(e recv.LogEntry) bool {
		minute := e.Timestamp.Truncate(time.Minute)
		rates[minute]++

		if IsError(e.Message) {
			normalized := NormalizeMessage(e.Message)
			errorCounts[normalized]++
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	// Sort errors by count descending, take top 20
	errors := make([]ErrorSummary, 0, len(errorCounts))
	for pat, count := range errorCounts {
		errors = append(errors, ErrorSummary{Pattern: pat, Count: count})
	}
	sort.Slice(errors, func(i, j int) bool { return errors[i].Count > errors[j].Count })
	if len(errors) > 20 {
		errors = errors[:20]
	}

	return &captureData{
		summary: DiffCapture{
			Dir:       dir,
			Started:   meta.Started,
			Stopped:   meta.Stopped,
			Duration:  duration,
			Lines:     meta.TotalLines,
			LinesPerS: linesPerSec,
			Labels:    labels,
		},
		errors: errors,
		rates:  rates,
	}, nil
}

func buildRateComparison(ratesA, ratesB map[time.Time]int64) []RateBucket {
	allMinutes := make(map[time.Time]bool)
	for m := range ratesA {
		allMinutes[m] = true
	}
	for m := range ratesB {
		allMinutes[m] = true
	}

	buckets := make([]RateBucket, 0, len(allMinutes))
	for m := range allMinutes {
		buckets = append(buckets, RateBucket{
			Minute: m,
			RateA:  ratesA[m],
			RateB:  ratesB[m],
		})
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Minute.Before(buckets[j].Minute) })
	return buckets
}

func setFromSlice(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}
