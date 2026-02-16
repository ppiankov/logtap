package archive

import (
	"fmt"
	"html/template"
	"io"
	"sort"
	"strings"
)

// htmlError holds pre-formatted error data for the HTML template.
type htmlError struct {
	Rank      int
	Signature string
	Count     string
	Pct       string
	FirstSeen string
	Example   string
}

// htmlTalkerEntry holds a single talker bar for the HTML template.
type htmlTalkerEntry struct {
	Value      string
	TotalLines string
	Pct        string
	ErrorLines string
	BarWidth   float64 // percentage width for total bar
	ErrWidth   float64 // percentage width for error bar
}

// htmlTalkerGroup holds all talker entries for one label key.
type htmlTalkerGroup struct {
	Key     string
	Entries []htmlTalkerEntry
}

// htmlSlice holds a recommended slice command for the HTML template.
type htmlSlice struct {
	Desc    string
	Command string
}

// htmlData holds all pre-computed data for the HTML template.
type htmlData struct {
	Dir        string
	Period     string
	TotalLines string
	ErrorLines string
	ErrorPct   string
	HasSignal  bool
	Signal     *TriageWindows
	HasChart   bool
	Timeline   template.HTML
	Errors     []htmlError
	Talkers    []htmlTalkerGroup
	Slices     []htmlSlice
}

// WriteHTML writes a self-contained HTML triage report.
func (r *TriageResult) WriteHTML(w io.Writer) error {
	data := r.buildHTMLData()
	return htmlTmpl.Execute(w, data)
}

func (r *TriageResult) buildHTMLData() htmlData {
	d := htmlData{
		Dir:        r.Dir,
		TotalLines: FormatCount(r.TotalLines),
		ErrorLines: FormatCount(r.ErrorLines),
	}

	if r.TotalLines > 0 {
		d.ErrorPct = fmt.Sprintf("%.1f%%", float64(r.ErrorLines)/float64(r.TotalLines)*100)
	} else {
		d.ErrorPct = "0.0%"
	}

	// period
	if r.Meta != nil && !r.Meta.Started.IsZero() {
		start := r.Meta.Started.Format("2006-01-02 15:04")
		if !r.Meta.Stopped.IsZero() {
			stop := r.Meta.Stopped.Format("15:04")
			dur := r.Meta.Stopped.Sub(r.Meta.Started)
			d.Period = fmt.Sprintf("%s — %s (%s)", start, stop, formatHumanDuration(dur))
		} else {
			d.Period = start
		}
	}

	// incident signal
	if r.Windows.PeakError != nil || r.Windows.IncidentStart != nil {
		d.HasSignal = true
		d.Signal = &r.Windows
	}

	// timeline SVG
	if len(r.Timeline) >= 2 {
		d.HasChart = true
		d.Timeline = template.HTML(r.buildTimelineSVG())
	}

	// errors
	for i, e := range r.Errors {
		pct := float64(0)
		if r.ErrorLines > 0 {
			pct = float64(e.Count) / float64(r.ErrorLines) * 100
		}
		d.Errors = append(d.Errors, htmlError{
			Rank:      i + 1,
			Signature: e.Signature,
			Count:     FormatCount(e.Count),
			Pct:       fmt.Sprintf("%.1f%%", pct),
			FirstSeen: e.FirstSeen.Format("15:04:05"),
			Example:   e.Example,
		})
	}

	// talkers
	keys := make([]string, 0, len(r.Talkers))
	for k := range r.Talkers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		entries := r.Talkers[key]
		var maxLines int64
		for _, e := range entries {
			if e.TotalLines > maxLines {
				maxLines = e.TotalLines
			}
		}

		group := htmlTalkerGroup{Key: key}
		for _, e := range entries {
			pct := float64(0)
			if r.TotalLines > 0 {
				pct = float64(e.TotalLines) / float64(r.TotalLines) * 100
			}
			barWidth := float64(0)
			if maxLines > 0 {
				barWidth = float64(e.TotalLines) / float64(maxLines) * 100
			}
			errWidth := float64(0)
			if maxLines > 0 {
				errWidth = float64(e.ErrorLines) / float64(maxLines) * 100
			}
			group.Entries = append(group.Entries, htmlTalkerEntry{
				Value:      e.Value,
				TotalLines: FormatCount(e.TotalLines),
				Pct:        fmt.Sprintf("%.1f%%", pct),
				ErrorLines: FormatCount(e.ErrorLines),
				BarWidth:   barWidth,
				ErrWidth:   errWidth,
			})
		}
		d.Talkers = append(d.Talkers, group)
	}

	// recommended slices
	if r.Windows.PeakError != nil {
		d.Slices = append(d.Slices, htmlSlice{
			Desc:    "Peak error window: " + r.Windows.PeakError.Desc,
			Command: fmt.Sprintf("logtap slice %s --from %s --to %s --out ./incident", r.Dir, r.Windows.PeakError.From, r.Windows.PeakError.To),
		})
		if len(r.Errors) > 0 {
			sig := r.Errors[0].Signature
			if len(sig) > 40 {
				sig = sig[:40]
			}
			d.Slices = append(d.Slices, htmlSlice{
				Desc:    "Top error pattern",
				Command: fmt.Sprintf("logtap slice %s --grep %q --out ./top-error", r.Dir, sig),
			})
		}
	}

	return d
}

func (r *TriageResult) buildTimelineSVG() string {
	if len(r.Timeline) < 2 {
		return ""
	}

	const (
		width  = 800
		height = 200
		padL   = 60
		padR   = 20
		padT   = 10
		padB   = 30
		chartW = width - padL - padR
		chartH = height - padT - padB
	)

	// find max Y
	var maxY int64
	for _, b := range r.Timeline {
		if b.TotalLines > maxY {
			maxY = b.TotalLines
		}
		if b.ErrorLines > maxY {
			maxY = b.ErrorLines
		}
	}
	if maxY == 0 {
		maxY = 1
	}

	n := len(r.Timeline)
	xStep := float64(chartW) / float64(n-1)

	// build polyline points
	totalPoints := make([]string, n)
	errorPoints := make([]string, n)
	for i, b := range r.Timeline {
		x := float64(padL) + float64(i)*xStep
		yTotal := float64(padT) + float64(chartH)*(1-float64(b.TotalLines)/float64(maxY))
		yError := float64(padT) + float64(chartH)*(1-float64(b.ErrorLines)/float64(maxY))
		totalPoints[i] = fmt.Sprintf("%.1f,%.1f", x, yTotal)
		errorPoints[i] = fmt.Sprintf("%.1f,%.1f", x, yError)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<svg viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg" style="width:100%%;max-width:%dpx;height:auto">`, width, height, width))

	// grid lines
	for i := 0; i <= 4; i++ {
		y := float64(padT) + float64(chartH)*float64(i)/4.0
		val := maxY - maxY*int64(i)/4
		sb.WriteString(fmt.Sprintf(`<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#e0e0e0" stroke-width="1"/>`, padL, y, width-padR, y))
		sb.WriteString(fmt.Sprintf(`<text x="%d" y="%.1f" text-anchor="end" font-size="11" fill="#666">%s</text>`, padL-5, y+4, FormatCount(val)))
	}

	// x-axis labels (every Nth bucket)
	labelEvery := 1
	if n > 20 {
		labelEvery = n / 10
	} else if n > 10 {
		labelEvery = 2
	}
	for i := 0; i < n; i += labelEvery {
		x := float64(padL) + float64(i)*xStep
		label := r.Timeline[i].Time.Format("15:04")
		sb.WriteString(fmt.Sprintf(`<text x="%.1f" y="%d" text-anchor="middle" font-size="10" fill="#666">%s</text>`, x, height-5, label))
	}

	// total lines polyline (blue)
	sb.WriteString(fmt.Sprintf(`<polyline points="%s" fill="none" stroke="#3b82f6" stroke-width="2"/>`, strings.Join(totalPoints, " ")))

	// error lines polyline (red)
	sb.WriteString(fmt.Sprintf(`<polyline points="%s" fill="none" stroke="#ef4444" stroke-width="2"/>`, strings.Join(errorPoints, " ")))

	// legend
	sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="12" height="3" fill="#3b82f6"/>`, width-padR-120, padT))
	sb.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-size="11" fill="#666">Total lines</text>`, width-padR-104, padT+4))
	sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="12" height="3" fill="#ef4444"/>`, width-padR-120, padT+14))
	sb.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-size="11" fill="#666">Errors</text>`, width-padR-104, padT+18))

	sb.WriteString(`</svg>`)
	return sb.String()
}

var htmlTmpl = template.Must(template.New("triage").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Triage Report: {{.Dir}}</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; color: #1a1a1a; background: #fafafa; padding: 2rem; max-width: 960px; margin: 0 auto; line-height: 1.5; }
  h1 { font-size: 1.4rem; margin-bottom: 0.5rem; }
  h2 { font-size: 1.1rem; margin: 1.5rem 0 0.75rem; border-bottom: 2px solid #e5e7eb; padding-bottom: 0.25rem; }
  .meta { color: #555; font-size: 0.9rem; margin-bottom: 0.25rem; }
  .stat { display: inline-block; background: #f3f4f6; border-radius: 6px; padding: 0.3rem 0.7rem; margin: 0.25rem 0.25rem 0.25rem 0; font-size: 0.85rem; }
  .stat-error { background: #fef2f2; color: #b91c1c; }
  .signal { background: #fffbeb; border: 1px solid #fcd34d; border-radius: 6px; padding: 0.75rem 1rem; margin: 0.75rem 0; font-size: 0.9rem; }
  .signal strong { color: #92400e; }
  table { width: 100%; border-collapse: collapse; font-size: 0.85rem; margin: 0.5rem 0; }
  th { text-align: left; background: #f9fafb; border-bottom: 2px solid #e5e7eb; padding: 0.4rem 0.6rem; font-weight: 600; }
  td { padding: 0.35rem 0.6rem; border-bottom: 1px solid #f3f4f6; }
  tr:hover { background: #f9fafb; }
  .sig { font-family: "SF Mono", Monaco, Consolas, monospace; font-size: 0.8rem; word-break: break-all; }
  .num { text-align: right; white-space: nowrap; }
  .chart-container { margin: 0.75rem 0; }
  .talker-group { margin: 0.75rem 0; }
  .talker-group h3 { font-size: 0.95rem; margin-bottom: 0.4rem; color: #374151; }
  .bar-row { display: flex; align-items: center; margin: 0.2rem 0; font-size: 0.85rem; }
  .bar-label { width: 140px; flex-shrink: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .bar-track { flex: 1; height: 20px; background: #f3f4f6; border-radius: 3px; position: relative; margin: 0 0.5rem; }
  .bar-fill { height: 100%; background: #3b82f6; border-radius: 3px; position: absolute; top: 0; left: 0; }
  .bar-err { height: 100%; background: #ef4444; border-radius: 3px; position: absolute; top: 0; left: 0; }
  .bar-val { width: 100px; flex-shrink: 0; text-align: right; font-size: 0.8rem; color: #555; }
  .slice { background: #f0fdf4; border: 1px solid #bbf7d0; border-radius: 6px; padding: 0.75rem 1rem; margin: 0.5rem 0; }
  .slice-desc { font-size: 0.85rem; color: #166534; margin-bottom: 0.3rem; }
  .slice pre { background: #1a1a1a; color: #e5e7eb; padding: 0.5rem 0.75rem; border-radius: 4px; font-size: 0.8rem; overflow-x: auto; }
  .empty { color: #9ca3af; font-style: italic; padding: 1rem 0; }
  footer { margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #e5e7eb; font-size: 0.8rem; color: #9ca3af; }
  @media print { body { padding: 1rem; } .slice pre { background: #f3f4f6; color: #1a1a1a; } }
</style>
</head>
<body>

<h1>Triage Report</h1>
<div class="meta">{{.Dir}}</div>
{{if .Period}}<div class="meta">{{.Period}}</div>{{end}}
<div>
  <span class="stat">{{.TotalLines}} lines</span>
  <span class="stat stat-error">{{.ErrorLines}} errors ({{.ErrorPct}})</span>
</div>

{{if .HasSignal}}
<h2>Incident Signal</h2>
<div class="signal">
{{if .Signal.PeakError}}<div><strong>Peak error window:</strong> {{.Signal.PeakError.From}} — {{.Signal.PeakError.To}} ({{.Signal.PeakError.Desc}})</div>{{end}}
{{if .Signal.IncidentStart}}<div><strong>Incident start:</strong> {{.Signal.IncidentStart.From}} ({{.Signal.IncidentStart.Desc}})</div>{{end}}
{{if .Signal.SteadyState}}<div><strong>Steady state:</strong> {{.Signal.SteadyState.From}} — {{.Signal.SteadyState.To}} ({{.Signal.SteadyState.Desc}})</div>{{end}}
</div>
{{end}}

<h2>Timeline</h2>
{{if .HasChart}}
<div class="chart-container">{{.Timeline}}</div>
{{else}}
<div class="empty">Not enough data for timeline chart.</div>
{{end}}

<h2>Top Errors</h2>
{{if .Errors}}
<table>
<thead><tr><th>#</th><th>Signature</th><th class="num">Count</th><th class="num">%</th><th class="num">First Seen</th></tr></thead>
<tbody>
{{range .Errors}}<tr><td>{{.Rank}}</td><td class="sig" title="{{.Example}}">{{.Signature}}</td><td class="num">{{.Count}}</td><td class="num">{{.Pct}}</td><td class="num">{{.FirstSeen}}</td></tr>
{{end}}</tbody>
</table>
{{else}}
<div class="empty">No errors found.</div>
{{end}}

{{if .Talkers}}
<h2>Top Talkers</h2>
{{range .Talkers}}
<div class="talker-group">
<h3>{{.Key}}</h3>
{{range .Entries}}
<div class="bar-row">
  <span class="bar-label" title="{{.Value}}">{{.Value}}</span>
  <div class="bar-track">
    <div class="bar-fill" style="width:{{printf "%.1f" .BarWidth}}%"></div>
    <div class="bar-err" style="width:{{printf "%.1f" .ErrWidth}}%"></div>
  </div>
  <span class="bar-val">{{.TotalLines}} ({{.Pct}})</span>
</div>
{{end}}
</div>
{{end}}
{{end}}

{{if .Slices}}
<h2>Recommended Slices</h2>
{{range .Slices}}
<div class="slice">
  <div class="slice-desc">{{.Desc}}</div>
  <pre>{{.Command}}</pre>
</div>
{{end}}
{{end}}

<footer>Generated by logtap triage</footer>
</body>
</html>
`))
