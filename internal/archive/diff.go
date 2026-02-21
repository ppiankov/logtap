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
	summary    DiffCapture
	errors     []ErrorSummary
	allErrors  map[string]int64    // full error counts (not truncated)
	errorLines int64               // total lines matching IsError
	rates      map[time.Time]int64 // per-minute counts
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
	var errorLines int64

	_, err = r.Scan(nil, func(e recv.LogEntry) bool {
		minute := e.Timestamp.Truncate(time.Minute)
		rates[minute]++

		if IsError(e.Message) {
			errorLines++
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
		errors:     errors,
		allErrors:  errorCounts,
		errorLines: errorLines,
		rates:      rates,
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

// BaselineDiffResult holds a verdict-oriented comparison against a baseline capture.
type BaselineDiffResult struct {
	Baseline         string       `json:"baseline"`
	Current          string       `json:"current"`
	ErrorRateChange  string       `json:"error_rate_change"`
	VolumeChange     string       `json:"volume_change"`
	NewErrorPatterns []ErrorDelta `json:"new_error_patterns,omitempty"`
	MissingLabels    []string     `json:"missing_labels,omitempty"`
	NewLabels        []string     `json:"new_labels,omitempty"`
	Verdict          string       `json:"verdict"`
	Confidence       float64      `json:"confidence"`
}

// ErrorDelta describes an error pattern that is new or significantly worse in the current capture.
type ErrorDelta struct {
	Pattern       string `json:"pattern"`
	Count         int64  `json:"count"`
	BaselineCount int64  `json:"baseline_count"`
}

// BaselineDiff compares a current capture against a baseline, producing a verdict.
// baselineDir is the known-good reference; currentDir is the capture under evaluation.
func BaselineDiff(baselineDir, currentDir string) (*BaselineDiffResult, error) {
	baseCap, err := summarizeCapture(baselineDir)
	if err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}
	curCap, err := summarizeCapture(currentDir)
	if err != nil {
		return nil, fmt.Errorf("current: %w", err)
	}

	result := &BaselineDiffResult{
		Baseline: baselineDir,
		Current:  currentDir,
	}

	// Error rates
	baseErrorRate := errorRate(baseCap.errorLines, baseCap.summary.Lines)
	curErrorRate := errorRate(curCap.errorLines, curCap.summary.Lines)
	errorRateChangePct := percentChange(baseErrorRate, curErrorRate)
	result.ErrorRateChange = formatPercentChange(errorRateChangePct)

	// Volume change
	volumeChangePct := percentChange(float64(baseCap.summary.Lines), float64(curCap.summary.Lines))
	result.VolumeChange = formatPercentChange(volumeChangePct)

	// New or significantly worse error patterns
	for pat, count := range curCap.allErrors {
		baseCount := baseCap.allErrors[pat]
		if baseCount == 0 {
			// entirely new pattern
			result.NewErrorPatterns = append(result.NewErrorPatterns, ErrorDelta{
				Pattern:       pat,
				Count:         count,
				BaselineCount: 0,
			})
		} else if count > baseCount*2 {
			// more than 2x increase
			result.NewErrorPatterns = append(result.NewErrorPatterns, ErrorDelta{
				Pattern:       pat,
				Count:         count,
				BaselineCount: baseCount,
			})
		}
	}
	sort.Slice(result.NewErrorPatterns, func(i, j int) bool {
		return result.NewErrorPatterns[i].Count > result.NewErrorPatterns[j].Count
	})

	// Label diffs (baseline perspective: missing = was in baseline but gone; new = appeared)
	baseLabels := setFromSlice(baseCap.summary.Labels)
	curLabels := setFromSlice(curCap.summary.Labels)
	for _, l := range baseCap.summary.Labels {
		if !curLabels[l] {
			result.MissingLabels = append(result.MissingLabels, l)
		}
	}
	for _, l := range curCap.summary.Labels {
		if !baseLabels[l] {
			result.NewLabels = append(result.NewLabels, l)
		}
	}

	// Verdict classification (deterministic)
	result.Verdict, result.Confidence = classifyVerdict(errorRateChangePct, volumeChangePct, result.NewErrorPatterns)

	return result, nil
}

// errorRate computes error percentage. Returns 0 if totalLines is 0.
func errorRate(errorLines, totalLines int64) float64 {
	if totalLines == 0 {
		return 0
	}
	return float64(errorLines) / float64(totalLines) * 100
}

// percentChange computes the percentage change from before to after.
// Returns 0 if before is 0 and after is 0. Returns +100 if before is 0 and after > 0.
func percentChange(before, after float64) float64 {
	if before == 0 {
		if after == 0 {
			return 0
		}
		return 100
	}
	return (after - before) / before * 100
}

// formatPercentChange formats a percentage change as "+340%" or "-20%".
func formatPercentChange(pct float64) string {
	if pct >= 0 {
		return fmt.Sprintf("+%.0f%%", pct)
	}
	return fmt.Sprintf("%.0f%%", pct)
}

// classifyVerdict returns a deterministic verdict and confidence score.
func classifyVerdict(errorRateChangePct, volumeChangePct float64, newPatterns []ErrorDelta) (string, float64) {
	hasNewPatterns := len(newPatterns) > 0

	// improvement: error rate decreased more than 20%
	if errorRateChangePct < -20 {
		confidence := 0.7
		if errorRateChangePct < -50 {
			confidence = 0.9
		}
		return "improvement", confidence
	}

	// regression: error rate increased more than 50% AND new error patterns exist
	if errorRateChangePct > 50 && hasNewPatterns {
		confidence := 0.7
		if errorRateChangePct > 200 {
			confidence = 0.95
		} else if errorRateChangePct > 100 {
			confidence = 0.85
		}
		return "regression", confidence
	}

	// different: significant volume change (>50%) but error rate within bounds,
	// or error rate increased but no new patterns
	absErrorChange := errorRateChangePct
	if absErrorChange < 0 {
		absErrorChange = -absErrorChange
	}
	absVolumeChange := volumeChangePct
	if absVolumeChange < 0 {
		absVolumeChange = -absVolumeChange
	}

	if absVolumeChange > 50 && absErrorChange <= 50 {
		return "different", 0.6
	}
	if errorRateChangePct > 50 && !hasNewPatterns {
		return "different", 0.5
	}

	// stable: error rate change within 20% AND no new patterns
	if absErrorChange <= 20 && !hasNewPatterns {
		confidence := 0.8
		if absErrorChange <= 5 {
			confidence = 0.95
		}
		return "stable", confidence
	}

	// borderline cases that don't fit cleanly
	if hasNewPatterns {
		return "different", 0.5
	}
	return "stable", 0.5
}

// WriteJSON writes the baseline diff result as JSON.
func (b *BaselineDiffResult) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}

// WriteText writes a human-readable baseline diff summary.
func (b *BaselineDiffResult) WriteText(w io.Writer) {
	tw := &textWriter{w: w}

	tw.printf("Baseline: %s\n", b.Baseline)
	tw.printf("Current:  %s\n", b.Current)
	tw.printf("\nVerdict:    %s (confidence %.0f%%)\n", b.Verdict, b.Confidence*100)
	tw.printf("Error rate: %s\n", b.ErrorRateChange)
	tw.printf("Volume:     %s\n", b.VolumeChange)

	if len(b.NewErrorPatterns) > 0 {
		tw.printf("\nNew/worse error patterns:\n")
		for _, e := range b.NewErrorPatterns {
			if e.BaselineCount == 0 {
				tw.printf("  [NEW %d] %s\n", e.Count, e.Pattern)
			} else {
				tw.printf("  [%d -> %d] %s\n", e.BaselineCount, e.Count, e.Pattern)
			}
		}
	}

	if len(b.MissingLabels) > 0 {
		tw.printf("\nMissing labels (in baseline, not in current): %v\n", b.MissingLabels)
	}
	if len(b.NewLabels) > 0 {
		tw.printf("\nNew labels (in current, not in baseline): %v\n", b.NewLabels)
	}
}
