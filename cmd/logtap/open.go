package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/ppiankov/logtap/internal/recv"
)

func newOpenCmd() *cobra.Command {
	var (
		speedStr    string
		fromStr     string
		toStr       string
		labels      []string
		grepStr     string
		dumpMode    bool
		dumpColor   string
		dumpContext int
		dumpBefore  int
		dumpAfter   int
		dumpHead    int
		dumpTail    int
		dumpCount   bool
		dumpFields  string
		injectSpecs []string
		injectAt    string
		injectDur   string
		injectOut   string
		jsonOutput  bool
	)

	cmd := &cobra.Command{
		Use:   "open <capture-dir>",
		Short: "Replay a capture directory",
		Long:  "Open a capture directory written by logtap recv and replay it in a TUI with speed control and filters.\nUse --inject to add synthetic faults to the replay stream.\nUse --inject-out to write the modified stream as a new capture directory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// default to instant speed when --grep is set and --speed was not explicit
			if grepStr != "" && !cmd.Flags().Changed("speed") {
				speedStr = "0"
			}
			dc := dumpConfig{
				enabled: dumpMode,
				color:   dumpColor,
				context: dumpContext,
				before:  dumpBefore,
				after:   dumpAfter,
				head:    dumpHead,
				tail:    dumpTail,
				count:   dumpCount,
				fields:  dumpFields,
				json:    jsonOutput,
			}
			return runOpen(args[0], speedStr, fromStr, toStr, labels, grepStr, dc,
				injectSpecs, injectAt, injectDur, injectOut, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&speedStr, "speed", "1", "replay speed: 0=instant, 1=realtime, 10=fast-forward (or 10x)")
	cmd.Flags().StringVar(&fromStr, "from", "", "start time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringVar(&toStr, "to", "", "end time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label filter (key=value, repeatable)")
	cmd.Flags().StringVar(&grepStr, "grep", "", "regex filter on log message")
	cmd.Flags().BoolVar(&dumpMode, "dump", false, "print matching lines to stdout (no TUI)")
	cmd.Flags().StringVar(&dumpColor, "color", "auto", "color output: auto, always, never (with --dump)")
	cmd.Flags().IntVarP(&dumpContext, "context", "C", 0, "lines of context around each grep match (with --dump)")
	cmd.Flags().IntVarP(&dumpBefore, "before", "B", 0, "lines of context before each grep match")
	cmd.Flags().IntVarP(&dumpAfter, "after", "A", 0, "lines of context after each grep match")
	cmd.Flags().IntVar(&dumpHead, "head", 0, "print only first N matching lines")
	cmd.Flags().IntVar(&dumpTail, "tail", 0, "print only last N matching lines")
	cmd.Flags().BoolVar(&dumpCount, "count", false, "print match count only (with --dump)")
	cmd.Flags().StringVar(&dumpFields, "fields", "", "comma-separated fields: ts,msg,app,<label-key>,all (with --dump)")
	cmd.Flags().StringArrayVar(&injectSpecs, "inject", nil, "fault to inject (error-spike, service-down=<svc>, latency-spike=<svc>)")
	cmd.Flags().StringVar(&injectAt, "at", "", "injection start time (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringVar(&injectDur, "duration", "1m", "injection duration (e.g. 30s, 1m, 5m)")
	cmd.Flags().StringVar(&injectOut, "inject-out", "", "write injected stream to new capture directory (skip TUI)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON (with --inject-out)")
	addFormatAlias(cmd, &jsonOutput)

	return cmd
}

func runOpen(dir, speedStr, fromStr, toStr string, labels []string, grepStr string, dc dumpConfig,
	injectSpecs []string, injectAt, injectDur, injectOut string, jsonOutput bool) error {

	reader, err := archive.NewReader(dir)
	if err != nil {
		return fmt.Errorf("open capture: %w", err)
	}
	meta := reader.Metadata()

	// parse speed
	speed, err := parseSpeed(speedStr)
	if err != nil {
		return fmt.Errorf("invalid --speed: %w", err)
	}

	// parse filters
	filter, err := buildFilter(fromStr, toStr, labels, grepStr, meta)
	if err != nil {
		return err
	}

	// dump mode — print to stdout, no TUI
	if dc.enabled {
		return runDump(reader, filter, dc)
	}

	// service summary for picker — skip if --label is set (already filtered)
	var services []archive.ServiceEntry
	if len(labels) == 0 {
		services = reader.ServiceSummary()
	}

	// parse fault injection
	if len(injectSpecs) > 0 {
		faults, err := parseInjectFlags(injectSpecs, injectAt, injectDur, meta)
		if err != nil {
			return err
		}

		if injectOut != "" {
			// output mode — skip TUI, write modified capture
			result, err := archive.InjectWrite(dir, injectOut, filter, faults)
			if err != nil {
				return fmt.Errorf("inject-out: %w", err)
			}
			if jsonOutput {
				return result.WriteJSON(os.Stdout)
			}
			result.WriteText(os.Stdout)
			return nil
		}

		// TUI mode — set transform on feeder after creation
		ring := recv.NewLogRing(0)
		totalLines := reader.TotalLines()
		feeder := archive.NewFeeder(reader, ring, filter, speed)
		feeder.SetTransform(archive.NewInjector(faults))
		model := archive.NewReplayModel(feeder, ring, meta, dir, totalLines, services)
		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("TUI: %w", err)
		}
		return nil
	}

	ring := recv.NewLogRing(0)
	totalLines := reader.TotalLines()
	feeder := archive.NewFeeder(reader, ring, filter, speed)
	model := archive.NewReplayModel(feeder, ring, meta, dir, totalLines, services)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI: %w", err)
	}
	return nil
}

// parseInjectFlags parses --inject, --at, --duration flags into FaultConfigs.
func parseInjectFlags(specs []string, atStr, durStr string, meta *recv.Metadata) ([]archive.FaultConfig, error) {
	refDate := meta.Started
	refTime := meta.Stopped
	if refTime.IsZero() {
		refTime = meta.Started
	}

	at, err := archive.ParseTimeFlag(atStr, refDate, refTime)
	if err != nil {
		return nil, fmt.Errorf("invalid --at: %w", err)
	}
	if at.IsZero() {
		// default to capture start
		at = meta.Started
	}

	dur, err := time.ParseDuration(durStr)
	if err != nil {
		return nil, fmt.Errorf("invalid --duration: %w", err)
	}

	var faults []archive.FaultConfig
	for _, spec := range specs {
		fc, err := archive.ParseFault(spec)
		if err != nil {
			return nil, fmt.Errorf("invalid --inject %q: %w", spec, err)
		}
		fc.At = at
		fc.Duration = dur
		faults = append(faults, fc)
	}
	return faults, nil
}

func parseSpeed(s string) (archive.Speed, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "x")
	var val float64
	if _, err := fmt.Sscanf(s, "%f", &val); err != nil {
		return 0, fmt.Errorf("invalid speed %q", s)
	}
	if val < 0 {
		return 0, fmt.Errorf("speed must be >= 0")
	}
	return archive.Speed(val), nil
}

type dumpConfig struct {
	enabled bool
	color   string
	context int
	before  int
	after   int
	head    int
	tail    int
	count   bool
	fields  string
	json    bool
}

func (d dumpConfig) useColor() bool {
	switch d.color {
	case "always":
		return true
	case "never":
		return false
	default: // "auto"
		return term.IsTerminal(int(os.Stdout.Fd()))
	}
}

func (d dumpConfig) beforeLines() int {
	if d.before > 0 {
		return d.before
	}
	return d.context
}

func (d dumpConfig) afterLines() int {
	if d.after > 0 {
		return d.after
	}
	return d.context
}

func runDump(reader *archive.Reader, filter *archive.Filter, dc dumpConfig) error {
	w := bufio.NewWriter(os.Stdout)
	defer func() { _ = w.Flush() }()

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	colorize := dc.useColor()
	grepRe := filter.Grep
	needContext := (dc.beforeLines() > 0 || dc.afterLines() > 0) && grepRe != nil

	// parse fields
	fields := parseFields(dc.fields)

	// --count mode
	if dc.count {
		return runDumpCount(reader, filter, dc.json, w, enc)
	}

	// --tail mode: collect last N in ring buffer
	if dc.tail > 0 {
		return runDumpTail(reader, filter, dc, w, enc, colorize, grepRe, fields)
	}

	// --context mode: need to scan without grep and apply it ourselves
	if needContext {
		return runDumpContext(reader, filter, dc, w, enc, colorize, grepRe, fields)
	}

	// standard dump
	var emitted int
	_, err := reader.Scan(filter, func(e recv.LogEntry) bool {
		writeDumpLine(w, enc, e, dc.json, colorize, grepRe, fields)
		emitted++
		if dc.head > 0 && emitted >= dc.head {
			return false
		}
		return true
	})
	return err
}

func runDumpCount(reader *archive.Reader, filter *archive.Filter, jsonOut bool, w *bufio.Writer, enc *json.Encoder) error {
	var count int64
	_, err := reader.Scan(filter, func(e recv.LogEntry) bool {
		count++
		return true
	})
	if err != nil {
		return err
	}
	if jsonOut {
		_ = enc.Encode(map[string]int64{"count": count})
	} else {
		_, _ = fmt.Fprintf(w, "%d\n", count)
	}
	return nil
}

func runDumpTail(reader *archive.Reader, filter *archive.Filter, dc dumpConfig,
	w *bufio.Writer, enc *json.Encoder, colorize bool, grepRe *regexp.Regexp, fields []string) error {

	ring := make([]recv.LogEntry, 0, dc.tail)
	_, err := reader.Scan(filter, func(e recv.LogEntry) bool {
		if len(ring) < dc.tail {
			ring = append(ring, e)
		} else {
			copy(ring, ring[1:])
			ring[dc.tail-1] = e
		}
		return true
	})
	if err != nil {
		return err
	}
	for _, e := range ring {
		writeDumpLine(w, enc, e, dc.json, colorize, grepRe, fields)
	}
	return nil
}

func runDumpContext(reader *archive.Reader, filter *archive.Filter, dc dumpConfig,
	w *bufio.Writer, enc *json.Encoder, colorize bool, grepRe *regexp.Regexp, fields []string) error {

	beforeN := dc.beforeLines()
	afterN := dc.afterLines()

	// scan without grep — build a filter without the grep component
	noGrepFilter := &archive.Filter{
		From:   filter.From,
		To:     filter.To,
		Labels: filter.Labels,
	}

	var beforeBuf []recv.LogEntry
	afterRemaining := 0
	lastPrinted := -1 // track for separator
	lineNum := 0
	emitted := 0

	_, err := reader.Scan(noGrepFilter, func(e recv.LogEntry) bool {
		idx := lineNum
		lineNum++

		isMatch := grepRe.MatchString(e.Message)

		if isMatch {
			// print before-context
			for _, be := range beforeBuf {
				if lastPrinted >= 0 {
					// check gap for separator
					_, _ = fmt.Fprintln(w, "--")
				}
				writeDumpLine(w, enc, be, dc.json, false, nil, fields)
				lastPrinted = idx
			}
			beforeBuf = nil

			if lastPrinted >= 0 && lastPrinted < idx-1 && afterRemaining == 0 {
				_, _ = fmt.Fprintln(w, "--")
			}
			writeDumpLine(w, enc, e, dc.json, colorize, grepRe, fields)
			lastPrinted = idx
			afterRemaining = afterN
			emitted++
			if dc.head > 0 && emitted >= dc.head {
				return false
			}
		} else if afterRemaining > 0 {
			writeDumpLine(w, enc, e, dc.json, false, nil, fields)
			lastPrinted = idx
			afterRemaining--
		} else {
			// add to before-context ring
			if beforeN > 0 {
				if len(beforeBuf) >= beforeN {
					beforeBuf = beforeBuf[1:]
				}
				beforeBuf = append(beforeBuf, e)
			}
		}
		return true
	})
	return err
}

func writeDumpLine(w *bufio.Writer, enc *json.Encoder, e recv.LogEntry,
	jsonOut, colorize bool, grepRe *regexp.Regexp, fields []string) {

	if jsonOut {
		_ = enc.Encode(e)
		return
	}

	line := formatDumpLine(e, fields)
	if colorize && grepRe != nil {
		line = grepRe.ReplaceAllStringFunc(line, func(s string) string {
			return "\033[1;31m" + s + "\033[0m"
		})
	}
	_, _ = fmt.Fprintln(w, line)
}

func formatDumpLine(e recv.LogEntry, fields []string) string {
	if len(fields) == 0 {
		// default: ts [app] message
		app := labelValue(e, "app")
		return fmt.Sprintf("%s [%s] %s", e.Timestamp.Format("2006-01-02T15:04:05Z"), app, e.Message)
	}

	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		switch f {
		case "ts":
			parts = append(parts, e.Timestamp.Format("2006-01-02T15:04:05Z"))
		case "msg":
			parts = append(parts, e.Message)
		case "all":
			// all labels sorted
			keys := make([]string, 0, len(e.Labels))
			for k := range e.Labels {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				parts = append(parts, fmt.Sprintf("%s=%s", k, e.Labels[k]))
			}
		default:
			// treat as label key
			parts = append(parts, e.Labels[f])
		}
	}
	return strings.Join(parts, "\t")
}

func labelValue(e recv.LogEntry, key string) string {
	if v, ok := e.Labels[key]; ok {
		return v
	}
	for _, v := range e.Labels {
		return v
	}
	return ""
}

func parseFields(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var fields []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			fields = append(fields, p)
		}
	}
	return fields
}
