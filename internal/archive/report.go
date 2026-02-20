package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

// ReportConfig controls report generation behavior.
type ReportConfig struct {
	Jobs int // parallel triage workers
	Top  int // top error signatures
}

// ReportResult is the single-artifact incident deliverable.
type ReportResult struct {
	Capture   ReportCapture  `json:"capture"`
	Labels    map[string]int `json:"labels"`
	Triage    ReportTriage   `json:"triage"`
	Severity  string         `json:"severity"`
	Suggested []string       `json:"suggested_commands,omitempty"`
}

// ReportCapture holds capture metadata for the report.
type ReportCapture struct {
	Dir             string    `json:"dir"`
	Files           int       `json:"files"`
	Entries         int64     `json:"entries"`
	Bytes           int64     `json:"bytes"`
	Started         time.Time `json:"started"`
	Stopped         time.Time `json:"stopped,omitempty"`
	DurationSeconds float64   `json:"duration_seconds"`
}

// ReportTriage holds triage results for the report.
type ReportTriage struct {
	TotalLines   int64            `json:"total_lines"`
	ErrorLines   int64            `json:"error_lines"`
	ErrorRatePct float64          `json:"error_rate_pct"`
	TopErrors    []ErrorSignature `json:"top_errors,omitempty"`
	Windows      TriageWindows    `json:"windows"`
}

// Report generates a combined inspect + triage result for a capture directory.
func Report(dir string, cfg ReportConfig, progress func(TriageProgress)) (*ReportResult, error) {
	// Inspect
	summary, err := Inspect(dir)
	if err != nil {
		return nil, fmt.Errorf("inspect: %w", err)
	}

	// Triage
	triageCfg := TriageConfig{
		Jobs: cfg.Jobs,
		Top:  cfg.Top,
	}
	triage, err := Triage(dir, triageCfg, progress)
	if err != nil {
		return nil, fmt.Errorf("triage: %w", err)
	}

	// Build report
	result := &ReportResult{
		Capture: buildReportCapture(dir, summary),
		Labels:  buildLabelSummary(summary),
		Triage:  buildReportTriage(triage),
	}

	result.Severity = classifySeverity(result.Triage.ErrorRatePct, triage.Errors)
	result.Suggested = buildSuggestions(dir, triage)

	return result, nil
}

func buildReportCapture(dir string, s *Summary) ReportCapture {
	rc := ReportCapture{
		Dir:     dir,
		Files:   s.Files,
		Entries: s.TotalLines,
		Bytes:   s.DiskSize,
	}
	if s.Meta != nil {
		rc.Started = s.Meta.Started
		rc.Stopped = s.Meta.Stopped
		if !rc.Started.IsZero() && !rc.Stopped.IsZero() {
			rc.DurationSeconds = rc.Stopped.Sub(rc.Started).Seconds()
		}
	}
	return rc
}

func buildLabelSummary(s *Summary) map[string]int {
	labels := make(map[string]int)
	for k, vals := range s.Labels {
		labels[k] = len(vals)
	}
	return labels
}

func buildReportTriage(t *TriageResult) ReportTriage {
	rt := ReportTriage{
		TotalLines: t.TotalLines,
		ErrorLines: t.ErrorLines,
		TopErrors:  t.Errors,
		Windows:    t.Windows,
	}
	if t.TotalLines > 0 {
		rt.ErrorRatePct = float64(t.ErrorLines) / float64(t.TotalLines) * 100
	}
	return rt
}

func classifySeverity(errorRatePct float64, errors []ErrorSignature) string {
	// Check for critical patterns
	for _, e := range errors {
		sig := e.Signature
		if containsAny(sig, "OOMKilled", "panic", "FATAL", "segfault", "core dumped") {
			return "high"
		}
	}
	switch {
	case errorRatePct > 5:
		return "high"
	case errorRatePct > 1:
		return "medium"
	default:
		return "low"
	}
}

func containsAny(s string, patterns ...string) bool {
	for _, p := range patterns {
		if len(s) >= len(p) {
			for i := 0; i <= len(s)-len(p); i++ {
				if s[i:i+len(p)] == p {
					return true
				}
			}
		}
	}
	return false
}

func buildSuggestions(dir string, t *TriageResult) []string {
	var suggestions []string

	if t.Windows.PeakError != nil {
		suggestions = append(suggestions,
			fmt.Sprintf("logtap slice %s --from %s --to %s --out ./peak-errors",
				dir, t.Windows.PeakError.From, t.Windows.PeakError.To))
	}

	if len(t.Errors) > 0 {
		suggestions = append(suggestions,
			fmt.Sprintf("logtap grep %q %s --format text",
				t.Errors[0].Signature, dir))
	}

	suggestions = append(suggestions,
		fmt.Sprintf("logtap export %s --format parquet --out capture.parquet", dir))

	return suggestions
}

// WriteJSON writes the report as indented JSON.
func (r *ReportResult) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteHTML writes a self-contained HTML report.
// It delegates to the triage HTML writer with an extended header.
func (r *ReportResult) WriteHTML(w io.Writer, triageResult *TriageResult, meta *recv.Metadata) error {
	p := func(s string) { _, _ = fmt.Fprintln(w, s) }
	pf := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }

	p("<!DOCTYPE html>")
	p(`<html lang="en"><head><meta charset="utf-8">`)
	p(`<title>logtap incident report</title>`)
	p(`<style>`)
	p(`body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; max-width: 960px; margin: 2em auto; padding: 0 1em; color: #1a1a2e; background: #fafafa; }`)
	p(`h1 { border-bottom: 2px solid #e0e0e0; padding-bottom: 0.5em; }`)
	p(`h2 { color: #16213e; margin-top: 2em; }`)
	p(`.meta-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1em; margin: 1em 0; }`)
	p(`.meta-card { background: white; border: 1px solid #e0e0e0; border-radius: 8px; padding: 1em; }`)
	p(`.meta-card .label { font-size: 0.85em; color: #666; text-transform: uppercase; }`)
	p(`.meta-card .value { font-size: 1.4em; font-weight: 600; margin-top: 0.3em; }`)
	p(`.severity-high { color: #d32f2f; } .severity-medium { color: #f57c00; } .severity-low { color: #388e3c; }`)
	p(`table { border-collapse: collapse; width: 100%; margin: 1em 0; }`)
	p(`th, td { border: 1px solid #e0e0e0; padding: 0.5em 0.8em; text-align: left; }`)
	p(`th { background: #f5f5f5; }`)
	p(`pre { background: #263238; color: #eeffff; padding: 1em; border-radius: 6px; overflow-x: auto; }`)
	p(`code { font-family: "SF Mono", "Fira Code", monospace; font-size: 0.9em; }`)
	p(`</style></head><body>`)

	p(`<h1>logtap incident report</h1>`)

	// Summary cards
	p(`<div class="meta-grid">`)
	pf(`<div class="meta-card"><div class="label">Severity</div><div class="value severity-%s">%s</div></div>`+"\n",
		r.Severity, r.Severity)
	pf(`<div class="meta-card"><div class="label">Entries</div><div class="value">%s</div></div>`+"\n",
		FormatCount(r.Capture.Entries))
	pf(`<div class="meta-card"><div class="label">Error Rate</div><div class="value">%.1f%%</div></div>`+"\n",
		r.Triage.ErrorRatePct)
	pf(`<div class="meta-card"><div class="label">Files</div><div class="value">%d</div></div>`+"\n",
		r.Capture.Files)
	if r.Capture.DurationSeconds > 0 {
		dur := time.Duration(r.Capture.DurationSeconds * float64(time.Second))
		pf(`<div class="meta-card"><div class="label">Duration</div><div class="value">%s</div></div>`+"\n",
			dur.Truncate(time.Second))
	}
	p(`</div>`)

	// Capture info
	p(`<h2>Capture</h2>`)
	pf("<p>Directory: <code>%s</code></p>\n", r.Capture.Dir)
	if !r.Capture.Started.IsZero() {
		pf("<p>Started: %s</p>\n", r.Capture.Started.Format(time.RFC3339))
	}
	if !r.Capture.Stopped.IsZero() {
		pf("<p>Stopped: %s</p>\n", r.Capture.Stopped.Format(time.RFC3339))
	}

	// Top errors table
	if len(r.Triage.TopErrors) > 0 {
		p(`<h2>Top Errors</h2>`)
		p(`<table><thead><tr><th>Signature</th><th>Count</th><th>First Seen</th></tr></thead><tbody>`)
		limit := len(r.Triage.TopErrors)
		if limit > 20 {
			limit = 20
		}
		for _, e := range r.Triage.TopErrors[:limit] {
			pf("<tr><td><code>%s</code></td><td>%d</td><td>%s</td></tr>\n",
				e.Signature, e.Count, e.FirstSeen.Format("15:04:05"))
		}
		p(`</tbody></table>`)
	}

	// Suggested commands
	if len(r.Suggested) > 0 {
		p(`<h2>Suggested Commands</h2>`)
		p(`<pre><code>`)
		for _, cmd := range r.Suggested {
			p(cmd)
		}
		p(`</code></pre>`)
	}

	// Embed triage timeline if available
	if triageResult != nil {
		p(`<h2>Timeline</h2>`)
		_ = triageResult.WriteHTML(w)
	}

	p(`</body></html>`)
	return nil
}
