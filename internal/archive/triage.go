package archive

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/ppiankov/logtap/internal/recv"
)

// TriageConfig controls triage behavior.
type TriageConfig struct {
	Jobs   int           // parallel workers (default runtime.NumCPU())
	Window time.Duration // histogram bucket width (default 1m)
	Top    int           // top error signatures (default 50)
}

// TriageProgress reports progress during triage scanning.
type TriageProgress struct {
	Scanned int64
	Total   int64
}

// TriageResult holds all triage output data.
type TriageResult struct {
	Dir          string                   `json:"dir"`
	Meta         *recv.Metadata           `json:"metadata,omitempty"`
	Timeline     []TriageBucket           `json:"timeline,omitempty"`
	Errors       []ErrorSignature         `json:"errors,omitempty"`
	Talkers      map[string][]TalkerEntry `json:"talkers,omitempty"`
	Windows      TriageWindows            `json:"windows"`
	Correlations []Correlation            `json:"correlations,omitempty"`
	TotalLines   int64                    `json:"total_lines"`
	ErrorLines   int64                    `json:"error_lines"`
}

// TriageBucket represents one time window in the histogram.
type TriageBucket struct {
	Time       time.Time `json:"time"`
	TotalLines int64     `json:"total_lines"`
	ErrorLines int64     `json:"error_lines"`
}

// ErrorSignature represents a normalized error pattern.
type ErrorSignature struct {
	Signature string    `json:"signature"`
	Count     int64     `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	Example   string    `json:"example"`
}

// TalkerEntry represents volume per label value.
type TalkerEntry struct {
	Value      string `json:"value"`
	TotalLines int64  `json:"total_lines"`
	ErrorLines int64  `json:"error_lines"`
}

// TriageWindows holds recommended time windows for slicing.
type TriageWindows struct {
	PeakError     *TimeWindow `json:"peak_error,omitempty"`
	IncidentStart *TimeWindow `json:"incident_start,omitempty"`
	SteadyState   *TimeWindow `json:"steady_state,omitempty"`
}

// TimeWindow represents a recommended time range.
type TimeWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
	Desc string `json:"description"`
}

// fileResult holds per-file scan results (no concurrent access).
type fileResult struct {
	totalLines int64
	errorLines int64
	buckets    map[int64]*bucketCount             // minute unix → counts
	signatures map[string]*sigAccum               // normalized → accumulator
	talkers    map[string]map[string]*talkerAccum // label key → value → accumulator
}

type bucketCount struct {
	total int64
	errs  int64
}

type sigAccum struct {
	count     int64
	firstSeen time.Time
	example   string
}

type talkerAccum struct {
	total int64
	errs  int64
}

func newFileResult() *fileResult {
	return &fileResult{
		buckets:    make(map[int64]*bucketCount),
		signatures: make(map[string]*sigAccum),
		talkers:    make(map[string]map[string]*talkerAccum),
	}
}

// Triage scans a capture directory for anomalies and produces a summary report.
func Triage(src string, cfg TriageConfig, progress func(TriageProgress)) (*TriageResult, error) {
	if cfg.Jobs <= 0 {
		cfg.Jobs = runtime.NumCPU()
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}
	if cfg.Top <= 0 {
		cfg.Top = 50
	}

	reader, err := NewReader(src)
	if err != nil {
		return nil, fmt.Errorf("open source: %w", err)
	}

	files := reader.Files()
	totalLines := reader.TotalLines()

	// pass 1: parallel scan (skips rotated files gracefully)
	results, err := parallelScan(files, cfg.Jobs, totalLines, progress)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	// catch-up: re-read index to pick up files added during scan
	scannedSet := make(map[string]bool, len(files))
	for _, f := range files {
		scannedSet[f.Name] = true
	}
	if catchupReader, err := NewReader(src); err == nil {
		var newFiles []FileInfo
		for _, f := range catchupReader.Files() {
			if !scannedSet[f.Name] {
				newFiles = append(newFiles, f)
			}
		}
		if len(newFiles) > 0 {
			_, _ = fmt.Fprintf(os.Stderr, "\nCatch-up: scanning %d new files added during triage\n", len(newFiles))
			catchupResults, err := parallelScan(newFiles, cfg.Jobs, 0, nil)
			if err == nil {
				results = append(results, catchupResults...)
			}
		}
	}

	// merge file results
	merged := mergeResults(results)

	// build sorted timeline
	timeline := buildTriageTimeline(merged.buckets, cfg.Window)

	// build sorted errors (top N)
	errors := buildTopErrors(merged.signatures, cfg.Top)

	// build sorted talkers
	talkers := buildTopTalkers(merged.talkers)

	// pass 2: derive windows
	windows := deriveWindows(timeline, merged.signatures)

	// pass 3: cross-service error correlation
	correlations, _ := Correlate(src, 10*time.Second)

	result := &TriageResult{
		Dir:          src,
		Meta:         reader.Metadata(),
		Timeline:     timeline,
		Errors:       errors,
		Talkers:      talkers,
		Windows:      windows,
		Correlations: correlations,
		TotalLines:   merged.totalLines,
		ErrorLines:   merged.errorLines,
	}

	return result, nil
}

func parallelScan(files []FileInfo, jobs int, totalLines int64, progress func(TriageProgress)) ([]*fileResult, error) {
	if len(files) == 0 {
		return nil, nil
	}

	workers := jobs
	if workers > len(files) {
		workers = len(files)
	}

	fileCh := make(chan FileInfo, len(files))
	for _, f := range files {
		fileCh <- f
	}
	close(fileCh)

	resultCh := make(chan *fileResult, len(files))
	var scanErr atomic.Value
	var scanned atomic.Int64
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range fileCh {
				fr, err := scanFileForTriage(f)
				if err != nil {
					scanErr.Store(err)
					return
				}
				resultCh <- fr

				n := scanned.Add(fr.totalLines)
				if progress != nil && n%10000 < fr.totalLines {
					progress(TriageProgress{Scanned: n, Total: totalLines})
				}
			}
		}()
	}

	wg.Wait()
	close(resultCh)

	if v := scanErr.Load(); v != nil {
		return nil, v.(error)
	}

	var results []*fileResult
	for fr := range resultCh {
		results = append(results, fr)
	}

	// final progress
	if progress != nil {
		progress(TriageProgress{Scanned: scanned.Load(), Total: totalLines})
	}

	return results, nil
}

func scanFileForTriage(f FileInfo) (*fileResult, error) {
	file, err := os.Open(f.Path)
	if err != nil {
		if os.IsNotExist(err) {
			// File was rotated away during scan — skip gracefully.
			_, _ = fmt.Fprintf(os.Stderr, "\nSkipping rotated file: %s\n", f.Name)
			return newFileResult(), nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var r io.Reader = file
	if strings.HasSuffix(f.Name, ".zst") {
		dec, err := zstd.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("zstd open: %w", err)
		}
		defer dec.Close()
		r = dec
	}

	fr := newFileResult()
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

		fr.totalLines++
		isErr := IsError(entry.Message)
		if isErr {
			fr.errorLines++
		}

		// timeline bucket
		bucketKey := entry.Timestamp.Truncate(time.Minute).Unix()
		bc := fr.buckets[bucketKey]
		if bc == nil {
			bc = &bucketCount{}
			fr.buckets[bucketKey] = bc
		}
		bc.total++
		if isErr {
			bc.errs++
		}

		// error signature
		if isErr {
			sig := NormalizeMessage(entry.Message)
			sa := fr.signatures[sig]
			if sa == nil {
				sa = &sigAccum{firstSeen: entry.Timestamp, example: entry.Message}
				fr.signatures[sig] = sa
			}
			sa.count++
			if entry.Timestamp.Before(sa.firstSeen) {
				sa.firstSeen = entry.Timestamp
			}
		}

		// talkers
		for k, v := range entry.Labels {
			vals := fr.talkers[k]
			if vals == nil {
				vals = make(map[string]*talkerAccum)
				fr.talkers[k] = vals
			}
			ta := vals[v]
			if ta == nil {
				ta = &talkerAccum{}
				vals[v] = ta
			}
			ta.total++
			if isErr {
				ta.errs++
			}
		}
	}

	return fr, scanner.Err()
}

func mergeResults(results []*fileResult) *fileResult {
	merged := newFileResult()
	for _, fr := range results {
		merged.totalLines += fr.totalLines
		merged.errorLines += fr.errorLines

		for k, bc := range fr.buckets {
			mbc := merged.buckets[k]
			if mbc == nil {
				mbc = &bucketCount{}
				merged.buckets[k] = mbc
			}
			mbc.total += bc.total
			mbc.errs += bc.errs
		}

		for sig, sa := range fr.signatures {
			msa := merged.signatures[sig]
			if msa == nil {
				msa = &sigAccum{firstSeen: sa.firstSeen, example: sa.example}
				merged.signatures[sig] = msa
			}
			msa.count += sa.count
			if sa.firstSeen.Before(msa.firstSeen) {
				msa.firstSeen = sa.firstSeen
				msa.example = sa.example
			}
		}

		for key, vals := range fr.talkers {
			mvals := merged.talkers[key]
			if mvals == nil {
				mvals = make(map[string]*talkerAccum)
				merged.talkers[key] = mvals
			}
			for val, ta := range vals {
				mta := mvals[val]
				if mta == nil {
					mta = &talkerAccum{}
					mvals[val] = mta
				}
				mta.total += ta.total
				mta.errs += ta.errs
			}
		}
	}
	return merged
}

func buildTriageTimeline(buckets map[int64]*bucketCount, _ time.Duration) []TriageBucket {
	if len(buckets) == 0 {
		return nil
	}

	// find min/max
	var minKey, maxKey int64
	first := true
	for k := range buckets {
		if first || k < minKey {
			minKey = k
		}
		if first || k > maxKey {
			maxKey = k
		}
		first = false
	}

	// build continuous timeline
	n := int((maxKey-minKey)/60) + 1
	if n > 10080 { // cap at 1 week
		n = 10080
	}

	timeline := make([]TriageBucket, n)
	for i := range timeline {
		key := minKey + int64(i)*60
		t := time.Unix(key, 0).UTC()
		timeline[i].Time = t
		if bc := buckets[key]; bc != nil {
			timeline[i].TotalLines = bc.total
			timeline[i].ErrorLines = bc.errs
		}
	}
	return timeline
}

func buildTopErrors(signatures map[string]*sigAccum, top int) []ErrorSignature {
	if len(signatures) == 0 {
		return nil
	}

	errors := make([]ErrorSignature, 0, len(signatures))
	for sig, sa := range signatures {
		errors = append(errors, ErrorSignature{
			Signature: sig,
			Count:     sa.count,
			FirstSeen: sa.firstSeen,
			Example:   sa.example,
		})
	}

	sort.Slice(errors, func(i, j int) bool {
		return errors[i].Count > errors[j].Count
	})

	if len(errors) > top {
		errors = errors[:top]
	}
	return errors
}

func buildTopTalkers(talkers map[string]map[string]*talkerAccum) map[string][]TalkerEntry {
	if len(talkers) == 0 {
		return nil
	}

	result := make(map[string][]TalkerEntry, len(talkers))
	for key, vals := range talkers {
		entries := make([]TalkerEntry, 0, len(vals))
		for val, ta := range vals {
			entries = append(entries, TalkerEntry{
				Value:      val,
				TotalLines: ta.total,
				ErrorLines: ta.errs,
			})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].TotalLines > entries[j].TotalLines
		})
		result[key] = entries
	}
	return result
}

func deriveWindows(timeline []TriageBucket, signatures map[string]*sigAccum) TriageWindows {
	var w TriageWindows
	if len(timeline) == 0 {
		return w
	}

	// peak error: sliding 5-minute window with max error density
	windowSize := 5
	if windowSize > len(timeline) {
		windowSize = len(timeline)
	}

	var maxErrs int64
	maxIdx := 0
	var runSum int64
	for i := 0; i < windowSize; i++ {
		runSum += timeline[i].ErrorLines
	}
	maxErrs = runSum
	for i := windowSize; i < len(timeline); i++ {
		runSum += timeline[i].ErrorLines - timeline[i-windowSize].ErrorLines
		if runSum > maxErrs {
			maxErrs = runSum
			maxIdx = i - windowSize + 1
		}
	}
	if maxErrs > 0 {
		from := timeline[maxIdx].Time
		to := timeline[min(maxIdx+windowSize-1, len(timeline)-1)].Time.Add(time.Minute)
		w.PeakError = &TimeWindow{
			From: from.Format(time.RFC3339),
			To:   to.Format(time.RFC3339),
			Desc: fmt.Sprintf("%s errors in %d minutes", FormatCount(maxErrs), windowSize),
		}
	}

	// incident start: minute with most first-seen error signatures
	if len(signatures) > 0 {
		firstSeenByMin := make(map[int64]int)
		for _, sa := range signatures {
			key := sa.firstSeen.Truncate(time.Minute).Unix()
			firstSeenByMin[key]++
		}

		var bestKey int64
		var bestCount int
		for key, count := range firstSeenByMin {
			if count > bestCount {
				bestCount = count
				bestKey = key
			}
		}
		if bestCount > 0 {
			from := time.Unix(bestKey, 0).UTC()
			to := from.Add(time.Minute)
			w.IncidentStart = &TimeWindow{
				From: from.Format(time.RFC3339),
				To:   to.Format(time.RFC3339),
				Desc: fmt.Sprintf("%d new error signatures first appeared", bestCount),
			}
		}
	}

	// steady state: longest stretch of below-median error rate
	var errRates []int64
	for _, b := range timeline {
		errRates = append(errRates, b.ErrorLines)
	}
	sorted := make([]int64, len(errRates))
	copy(sorted, errRates)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	median := sorted[len(sorted)/2]

	var bestStart, bestLen, curStart, curLen int
	for i, rate := range errRates {
		if rate <= median {
			if curLen == 0 {
				curStart = i
			}
			curLen++
			if curLen > bestLen {
				bestLen = curLen
				bestStart = curStart
			}
		} else {
			curLen = 0
		}
	}

	if bestLen >= 3 { // require at least 3 minutes
		from := timeline[bestStart].Time
		to := timeline[bestStart+bestLen-1].Time.Add(time.Minute)
		w.SteadyState = &TimeWindow{
			From: from.Format(time.RFC3339),
			To:   to.Format(time.RFC3339),
			Desc: fmt.Sprintf("%d minutes at or below median error rate", bestLen),
		}
	}

	return w
}

// WriteSummary writes a human-readable triage report.
func (r *TriageResult) WriteSummary(w io.Writer) {
	tw := &textWriter{w: w}

	tw.printf("# Triage: %s\n\n", r.Dir)

	if r.Meta != nil && !r.Meta.Started.IsZero() {
		start := r.Meta.Started.Format("2006-01-02 15:04")
		if !r.Meta.Stopped.IsZero() {
			stop := r.Meta.Stopped.Format("15:04")
			dur := r.Meta.Stopped.Sub(r.Meta.Started)
			tw.printf("Period:  %s — %s (%s)\n", start, stop, formatHumanDuration(dur))
		} else {
			tw.printf("Period:  %s\n", start)
		}
	}

	tw.printf("Lines:   %s (%s errors)\n", FormatCount(r.TotalLines), FormatCount(r.ErrorLines))
	tw.println()

	// incident signal
	if r.Windows.PeakError != nil || r.Windows.IncidentStart != nil {
		tw.println("## Incident Signal")
		if r.Windows.PeakError != nil {
			tw.printf("Peak error window:  %s — %s (%s)\n",
				r.Windows.PeakError.From, r.Windows.PeakError.To, r.Windows.PeakError.Desc)
		}
		if r.Windows.IncidentStart != nil {
			tw.printf("Incident start:     %s (%s)\n",
				r.Windows.IncidentStart.From, r.Windows.IncidentStart.Desc)
		}
		tw.println()
	}

	// top errors
	if len(r.Errors) > 0 {
		tw.printf("## Top Errors (of %s total)\n", FormatCount(r.ErrorLines))
		for i, e := range r.Errors {
			pct := float64(0)
			if r.ErrorLines > 0 {
				pct = float64(e.Count) / float64(r.ErrorLines) * 100
			}
			tw.printf("  %d. %-60s %s  (%.1f%%)\n", i+1, e.Signature, FormatCount(e.Count), pct)
		}
		tw.println()
	}

	// top talkers (use "app" label key if available, fallback to first key)
	if len(r.Talkers) > 0 {
		tw.println("## Top Talkers")
		keys := make([]string, 0, len(r.Talkers))
		for k := range r.Talkers {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			entries := r.Talkers[key]
			tw.printf("  %s:\n", key)
			for _, e := range entries {
				pct := float64(0)
				if r.TotalLines > 0 {
					pct = float64(e.TotalLines) / float64(r.TotalLines) * 100
				}
				errPct := ""
				if e.ErrorLines > 0 && r.ErrorLines > 0 {
					errPct = fmt.Sprintf("  ← %.0f%% of errors", float64(e.ErrorLines)/float64(r.ErrorLines)*100)
				}
				tw.printf("    %-20s %s lines  (%.1f%%)%s\n",
					e.Value, FormatCount(e.TotalLines), pct, errPct)
			}
		}
		tw.println()
	}

	// cross-service correlations
	if len(r.Correlations) > 0 {
		tw.println("## Cross-Service Correlations")
		for _, c := range r.Correlations {
			tw.printf("  %s → %s  lag=%.0fs  pattern=%s  confidence=%.2f\n",
				c.Source, c.Target, c.LagSeconds, c.Pattern, c.Confidence)
		}
		tw.println()
	}

	// recommended slices
	if r.Windows.PeakError != nil {
		tw.println("## Recommended Slices")
		tw.printf("  logtap slice %s --from %s --to %s --out ./incident\n",
			r.Dir, r.Windows.PeakError.From, r.Windows.PeakError.To)
		if len(r.Errors) > 0 {
			// suggest grep for top error
			sig := r.Errors[0].Signature
			// truncate long signatures for the command suggestion
			if len(sig) > 40 {
				sig = sig[:40]
			}
			tw.printf("  logtap slice %s --grep %q --out ./top-error\n", r.Dir, sig)
		}
	}
}

// WriteTimeline writes a CSV histogram: minute,total_lines,error_lines.
func (r *TriageResult) WriteTimeline(w io.Writer) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"minute", "total_lines", "error_lines"})
	for _, b := range r.Timeline {
		_ = cw.Write([]string{
			b.Time.Format(time.RFC3339),
			fmt.Sprintf("%d", b.TotalLines),
			fmt.Sprintf("%d", b.ErrorLines),
		})
	}
	cw.Flush()
}

// WriteTopErrors writes ranked error signatures.
func (r *TriageResult) WriteTopErrors(w io.Writer) {
	tw := &textWriter{w: w}
	for i, e := range r.Errors {
		pct := float64(0)
		if r.ErrorLines > 0 {
			pct = float64(e.Count) / float64(r.ErrorLines) * 100
		}
		tw.printf("%d. %s\t%d\t(%.1f%%)\tfirst: %s\n",
			i+1, e.Signature, e.Count, pct, e.FirstSeen.Format(time.RFC3339))
	}
}

// WriteTopTalkers writes volume per label value, sorted by line count.
func (r *TriageResult) WriteTopTalkers(w io.Writer) {
	tw := &textWriter{w: w}
	keys := make([]string, 0, len(r.Talkers))
	for k := range r.Talkers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		tw.printf("%s:\n", key)
		for _, e := range r.Talkers[key] {
			pct := float64(0)
			if r.TotalLines > 0 {
				pct = float64(e.TotalLines) / float64(r.TotalLines) * 100
			}
			tw.printf("  %-20s %d lines\t(%.1f%%)\t%d errors\n",
				e.Value, e.TotalLines, pct, e.ErrorLines)
		}
	}
}

// WriteJSON writes the full triage result as JSON.
func (r *TriageResult) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteWindows writes recommended time windows as JSON.
func (r *TriageResult) WriteWindows(w io.Writer) error {
	data, err := json.MarshalIndent(r.Windows, "", "  ")
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
