package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

// maxSkewCorrection is the maximum clock offset we'll correct.
// Larger offsets likely indicate different time ranges, not clock skew.
const maxSkewCorrection = 60 * time.Second

// ClockCorrection describes a detected clock offset for one source.
type ClockCorrection struct {
	Source     string  `json:"source"`
	OffsetMs   int64   `json:"offset_ms"`
	Confidence float64 `json:"confidence"`
	Method     string  `json:"method"`
}

// sigTimes maps normalized error signature → first-seen timestamp.
type sigTimes map[string]time.Time

// skewSourceInfo holds scan results for one source during skew detection.
type skewSourceInfo struct {
	dir   string
	sigs  sigTimes
	lines int64
}

// DetectSkew compares error signature timestamps across sources to estimate clock offsets.
// Returns one ClockCorrection per non-reference source. Sources with no detectable
// skew or skew exceeding maxSkewCorrection are omitted.
func DetectSkew(sources []string) ([]ClockCorrection, error) {
	if len(sources) < 2 {
		return nil, nil
	}

	// scan each source for error signatures and their first-seen times
	infos := make([]skewSourceInfo, len(sources))
	for i, src := range sources {
		sigs, lines, err := scanErrorSignatures(src)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", src, err)
		}
		infos[i] = skewSourceInfo{dir: src, sigs: sigs, lines: lines}
	}

	// find reference: source with most lines
	refIdx := 0
	for i, info := range infos {
		if info.lines > infos[refIdx].lines {
			refIdx = i
		}
	}
	ref := infos[refIdx]

	var corrections []ClockCorrection
	for i, info := range infos {
		if i == refIdx {
			continue
		}

		offset, confidence, method := estimateOffset(ref, info)
		if confidence == 0 {
			continue
		}

		// skip if offset exceeds maximum
		if abs(offset) > maxSkewCorrection {
			_, _ = fmt.Fprintf(os.Stderr, "\nClock skew for %s exceeds 60s (%.1fs), skipping correction\n",
				info.dir, offset.Seconds())
			continue
		}

		corrections = append(corrections, ClockCorrection{
			Source:     info.dir,
			OffsetMs:   offset.Milliseconds(),
			Confidence: math.Round(confidence*100) / 100,
			Method:     method,
		})
	}

	return corrections, nil
}

func estimateOffset(ref, src skewSourceInfo) (time.Duration, float64, string) {
	// method 1: shared error signatures
	var offsets []time.Duration
	for sig, refTime := range ref.sigs {
		if srcTime, ok := src.sigs[sig]; ok {
			offsets = append(offsets, refTime.Sub(srcTime))
		}
	}

	if len(offsets) >= 1 {
		// sort and take median
		sort.Slice(offsets, func(i, j int) bool {
			return offsets[i] < offsets[j]
		})
		median := offsets[len(offsets)/2]

		// confidence scales with number of shared signatures
		confidence := math.Min(float64(len(offsets))/5.0, 1.0)
		if confidence < 0.5 {
			confidence = 0.5
		}
		return median, confidence, "error_signature"
	}

	// method 2: metadata overlap fallback
	refReader, err := NewReader(ref.dir)
	if err != nil {
		return 0, 0, ""
	}
	srcReader, err := NewReader(src.dir)
	if err != nil {
		return 0, 0, ""
	}

	refMeta := refReader.Metadata()
	srcMeta := srcReader.Metadata()
	if refMeta == nil || srcMeta == nil {
		return 0, 0, ""
	}
	if refMeta.Started.IsZero() || srcMeta.Started.IsZero() {
		return 0, 0, ""
	}

	offset := refMeta.Started.Sub(srcMeta.Started)
	return offset, 0.3, "metadata_overlap"
}

func scanErrorSignatures(dir string) (sigTimes, int64, error) {
	reader, err := NewReader(dir)
	if err != nil {
		return nil, 0, err
	}

	sigs := make(sigTimes)
	var totalLines int64

	_, err = reader.Scan(nil, func(e recv.LogEntry) bool {
		totalLines++
		if IsError(e.Message) {
			norm := NormalizeMessage(e.Message)
			if _, ok := sigs[norm]; !ok {
				sigs[norm] = e.Timestamp
			} else if e.Timestamp.Before(sigs[norm]) {
				sigs[norm] = e.Timestamp
			}
		}
		return true
	})
	if err != nil {
		return nil, 0, err
	}

	return sigs, totalLines, nil
}

// RewriteWithOffset reads entries from src, adjusts timestamps by offset, and writes to dst.
func RewriteWithOffset(src, dst string, offset time.Duration) (int64, error) {
	reader, err := NewReader(src)
	if err != nil {
		return 0, fmt.Errorf("open source: %w", err)
	}
	meta := reader.Metadata()

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, fmt.Errorf("create output dir: %w", err)
	}

	dataName := "corrected-000.jsonl"
	dataPath := filepath.Join(dst, dataName)
	dataFile, err := os.Create(dataPath)
	if err != nil {
		return 0, fmt.Errorf("create data file: %w", err)
	}
	defer func() { _ = dataFile.Close() }()

	w := bufio.NewWriter(dataFile)

	var totalLines, totalBytes int64
	var minTS, maxTS time.Time

	_, scanErr := reader.Scan(nil, func(e recv.LogEntry) bool {
		e.Timestamp = e.Timestamp.Add(offset)

		data, err := json.Marshal(e)
		if err != nil {
			return true // skip bad entries
		}
		line := append(data, '\n')
		if _, err := w.Write(line); err != nil {
			return false
		}
		totalLines++
		totalBytes += int64(len(line))

		if minTS.IsZero() || e.Timestamp.Before(minTS) {
			minTS = e.Timestamp
		}
		if maxTS.IsZero() || e.Timestamp.After(maxTS) {
			maxTS = e.Timestamp
		}
		return true
	})
	if scanErr != nil {
		return 0, fmt.Errorf("scan source: %w", scanErr)
	}

	if err := w.Flush(); err != nil {
		return 0, fmt.Errorf("flush data: %w", err)
	}

	// write metadata
	outMeta := &recv.Metadata{
		Version:    1,
		Format:     "jsonl",
		Started:    minTS,
		Stopped:    maxTS,
		TotalLines: totalLines,
		TotalBytes: totalBytes,
	}
	if meta != nil {
		outMeta.LabelsSeen = meta.LabelsSeen
		outMeta.Redaction = meta.Redaction
	}
	if err := recv.WriteMetadata(dst, outMeta); err != nil {
		return 0, fmt.Errorf("write metadata: %w", err)
	}

	// write index
	indexEntry := rotate.IndexEntry{
		File:  dataName,
		From:  minTS,
		To:    maxTS,
		Lines: totalLines,
		Bytes: totalBytes,
	}
	if err := writeIndexFile(dst, []rotate.IndexEntry{indexEntry}); err != nil {
		return 0, fmt.Errorf("write index: %w", err)
	}

	return totalLines, nil
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
