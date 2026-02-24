package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/ppiankov/logtap/internal/recv"
)

// Correlation represents a detected temporal error pattern between two services.
type Correlation struct {
	Source      string  `json:"source"`       // service that failed first
	Target      string  `json:"target"`       // service that failed after
	LagSeconds  float64 `json:"lag_seconds"`  // time between source and target errors
	Pattern     string  `json:"pattern"`      // cascade_timeout, cascade_error, co_failure
	Confidence  float64 `json:"confidence"`   // 0.0-1.0
	SourceError string  `json:"source_error"` // first error from source
	TargetError string  `json:"target_error"` // first error from target
}

// minConfidence is the threshold below which correlations are discarded.
const minConfidence = 0.5

// serviceErrors holds error occurrences for a single service, keyed by window bucket.
type serviceErrors struct {
	windows    map[int64]int64 // window unix → error count
	firstError string          // first error message seen
}

// Correlate analyzes error entries grouped by label to detect temporal cascade patterns.
func Correlate(dir string, windowSize time.Duration) ([]Correlation, error) {
	if windowSize <= 0 {
		windowSize = 10 * time.Second
	}

	reader, err := NewReader(dir)
	if err != nil {
		return nil, fmt.Errorf("open source: %w", err)
	}

	// pass 1: read all entries, group errors by service
	services := make(map[string]*serviceErrors)
	for _, f := range reader.Files() {
		if err := scanFileForCorrelation(f, windowSize, services); err != nil {
			return nil, fmt.Errorf("scan %s: %w", f.Name, err)
		}
	}

	// need at least 2 services with errors
	if len(services) < 2 {
		return nil, nil
	}

	// pass 2: for each ordered pair (A, B), compute cross-correlation
	var correlations []Correlation
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, srcName := range names {
		src := services[srcName]
		for j, tgtName := range names {
			if i == j {
				continue
			}
			tgt := services[tgtName]

			c := computeCorrelation(srcName, tgtName, src, tgt, windowSize)
			if c != nil && c.Confidence > minConfidence {
				correlations = append(correlations, *c)
			}
		}
	}

	// deduplicate: for each unordered pair, keep the higher-confidence direction
	correlations = deduplicateCorrelations(correlations)

	// sort by confidence descending
	sort.Slice(correlations, func(i, j int) bool {
		return correlations[i].Confidence > correlations[j].Confidence
	})

	return correlations, nil
}

func scanFileForCorrelation(f FileInfo, windowSize time.Duration, services map[string]*serviceErrors) error {
	file, err := os.Open(f.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // file rotated away during scan
		}
		return err
	}
	defer func() { _ = file.Close() }()

	var r io.Reader = file
	if strings.HasSuffix(f.Name, ".zst") {
		dec, err := zstd.NewReader(file)
		if err != nil {
			return fmt.Errorf("zstd open: %w", err)
		}
		defer dec.Close()
		r = dec
	}

	windowSec := int64(windowSize.Seconds())
	if windowSec <= 0 {
		windowSec = 10
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry recv.LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		if !IsError(entry.Message) {
			continue
		}

		svcName := serviceLabel(entry.Labels)
		if svcName == "" {
			continue
		}

		svc := services[svcName]
		if svc == nil {
			svc = &serviceErrors{
				windows:    make(map[int64]int64),
				firstError: entry.Message,
			}
			services[svcName] = svc
		}

		bucketKey := entry.Timestamp.Unix() / windowSec
		svc.windows[bucketKey]++
	}

	return scanner.Err()
}

// serviceLabel extracts the service name from labels. Uses "app" key, falls back to first label.
func serviceLabel(labels map[string]string) string {
	if v, ok := labels["app"]; ok && v != "" {
		return v
	}
	// fall back to first label value (sorted for determinism)
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		return labels[keys[0]]
	}
	return ""
}

func computeCorrelation(srcName, tgtName string, src, tgt *serviceErrors, windowSize time.Duration) *Correlation {
	// find overlapping time range
	var minBucket, maxBucket int64
	first := true
	for b := range src.windows {
		if first || b < minBucket {
			minBucket = b
		}
		if first || b > maxBucket {
			maxBucket = b
		}
		first = false
	}
	for b := range tgt.windows {
		if first || b < minBucket {
			minBucket = b
		}
		if first || b > maxBucket {
			maxBucket = b
		}
		first = false
	}

	if first {
		return nil // no data
	}

	nBuckets := int(maxBucket - minBucket + 1)
	if nBuckets < 1 {
		return nil
	}

	// build dense vectors
	srcVec := make([]float64, nBuckets)
	tgtVec := make([]float64, nBuckets)
	for b, count := range src.windows {
		idx := int(b - minBucket)
		if idx >= 0 && idx < nBuckets {
			srcVec[idx] = float64(count)
		}
	}
	for b, count := range tgt.windows {
		idx := int(b - minBucket)
		if idx >= 0 && idx < nBuckets {
			tgtVec[idx] = float64(count)
		}
	}

	// compute cross-correlation at offsets -maxLag to +maxLag
	maxLag := 5 // up to 5 windows of lag
	if maxLag > nBuckets-1 {
		maxLag = nBuckets - 1
	}

	bestCorr := 0.0
	bestLag := 0

	for lag := 0; lag <= maxLag; lag++ {
		corr := crossCorrelation(srcVec, tgtVec, lag)
		if corr > bestCorr {
			bestCorr = corr
			bestLag = lag
		}
	}

	if bestCorr <= minConfidence {
		return nil
	}

	lagSeconds := float64(bestLag) * windowSize.Seconds()

	// detect pattern
	pattern := classifyPattern(srcName, tgtName, src.firstError, tgt.firstError, lagSeconds, windowSize.Seconds())

	return &Correlation{
		Source:      srcName,
		Target:      tgtName,
		LagSeconds:  lagSeconds,
		Pattern:     pattern,
		Confidence:  math.Round(bestCorr*100) / 100,
		SourceError: src.firstError,
		TargetError: tgt.firstError,
	}
}

// crossCorrelation computes normalized cross-correlation between two vectors at a given lag.
// Positive lag means tgt is shifted forward (src leads tgt).
func crossCorrelation(src, tgt []float64, lag int) float64 {
	n := len(src)
	if n == 0 || lag >= n {
		return 0
	}

	// compute means
	var srcSum, tgtSum float64
	var count int
	for i := 0; i < n-lag; i++ {
		srcSum += src[i]
		tgtSum += tgt[i+lag]
		count++
	}
	if count == 0 {
		return 0
	}
	srcMean := srcSum / float64(count)
	tgtMean := tgtSum / float64(count)

	// compute normalized cross-correlation (Pearson)
	var num, srcVar, tgtVar float64
	for i := 0; i < n-lag; i++ {
		ds := src[i] - srcMean
		dt := tgt[i+lag] - tgtMean
		num += ds * dt
		srcVar += ds * ds
		tgtVar += dt * dt
	}

	denom := math.Sqrt(srcVar * tgtVar)
	if denom == 0 {
		// both vectors constant: if both have errors they co-occur
		if srcMean > 0 && tgtMean > 0 {
			return 1.0
		}
		return 0
	}

	corr := num / denom
	if corr < 0 {
		return 0 // negative correlation is not a cascade
	}
	return corr
}

// classifyPattern determines cascade type from context.
func classifyPattern(srcName, tgtName, srcError, tgtError string, lagSeconds, windowSeconds float64) string {
	// co_failure: lag within one window
	if lagSeconds < windowSeconds {
		return "co_failure"
	}

	// cascade_timeout: source error mentions target service name
	if mentionsService(srcError, tgtName) || mentionsService(tgtError, srcName) {
		return "cascade_timeout"
	}

	// default: temporal correlation without explicit mention
	return "cascade_error"
}

// mentionsService checks if a log message mentions a service name.
func mentionsService(msg, svcName string) bool {
	if svcName == "" || msg == "" {
		return false
	}
	pattern := fmt.Sprintf(`(?i)\b%s\b`, regexp.QuoteMeta(svcName))
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(msg)
}

// deduplicateCorrelations keeps only the higher-confidence direction for each pair.
func deduplicateCorrelations(correlations []Correlation) []Correlation {
	type pairKey struct{ a, b string }
	best := make(map[pairKey]*Correlation)

	for i := range correlations {
		c := &correlations[i]
		a, b := c.Source, c.Target
		if a > b {
			a, b = b, a
		}
		key := pairKey{a, b}
		if existing, ok := best[key]; !ok || c.Confidence > existing.Confidence {
			best[key] = c
		}
	}

	result := make([]Correlation, 0, len(best))
	for _, c := range best {
		result = append(result, *c)
	}
	return result
}
