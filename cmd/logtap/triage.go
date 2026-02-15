package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

func newTriageCmd() *cobra.Command {
	var (
		outDir     string
		jobs       int
		windowStr  string
		top        int
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "triage <capture-dir>",
		Short: "Scan capture for anomalies and produce a summary report",
		Long:  "Triage scans a capture directory for error patterns, volume spikes, and anomalies, producing a summary report with recommended slices.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			window, err := time.ParseDuration(windowStr)
			if err != nil {
				return fmt.Errorf("invalid --window: %w", err)
			}
			return runTriage(args[0], outDir, jobs, window, top, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&outDir, "out", "", "output directory for triage artifacts")
	cmd.Flags().IntVar(&jobs, "jobs", runtime.NumCPU(), "parallel scan workers")
	cmd.Flags().StringVar(&windowStr, "window", "1m", "histogram bucket width")
	cmd.Flags().IntVar(&top, "top", 50, "number of top error signatures")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON to stdout")

	return cmd
}

func runTriage(src, outDir string, jobs int, window time.Duration, top int, jsonOutput bool) error {
	triageCfg := archive.TriageConfig{
		Jobs:   jobs,
		Window: window,
		Top:    top,
	}

	progress := func(p archive.TriageProgress) {
		if p.Total > 0 {
			pct := float64(p.Scanned) / float64(p.Total) * 100
			fmt.Fprintf(os.Stderr, "\rTriage: %s / %s lines (%.1f%%)",
				archive.FormatCount(p.Scanned), archive.FormatCount(p.Total), pct)
		} else {
			fmt.Fprintf(os.Stderr, "\rTriage: %s lines", archive.FormatCount(p.Scanned))
		}
	}

	result, err := archive.Triage(src, triageCfg, progress)
	if err != nil {
		fmt.Fprintln(os.Stderr)
		return err
	}

	fmt.Fprintf(os.Stderr, "\rTriage: %s lines scanned, %s errors found\n",
		archive.FormatCount(result.TotalLines), archive.FormatCount(result.ErrorLines))

	if jsonOutput {
		return result.WriteJSON(os.Stdout)
	}

	if outDir == "" {
		return fmt.Errorf("--out is required (or use --json for stdout)")
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	outputs := []struct {
		name string
		fn   func(*os.File)
	}{
		{"summary.md", func(f *os.File) { result.WriteSummary(f) }},
		{"timeline.csv", func(f *os.File) { result.WriteTimeline(f) }},
		{"top_errors.txt", func(f *os.File) { result.WriteTopErrors(f) }},
		{"top_talkers.txt", func(f *os.File) { result.WriteTopTalkers(f) }},
		{"windows.json", func(f *os.File) { _ = result.WriteWindows(f) }},
	}

	for _, out := range outputs {
		path := filepath.Join(outDir, out.name)
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create %s: %w", out.name, err)
		}
		out.fn(f)
		if err := f.Close(); err != nil {
			return fmt.Errorf("write %s: %w", out.name, err)
		}
	}

	fmt.Fprintf(os.Stderr, "Results: %s\n", filepath.Join(outDir, "summary.md"))
	return nil
}
