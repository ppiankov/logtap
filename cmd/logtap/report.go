package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/ppiankov/logtap/internal/recv"
)

func newReportCmd() *cobra.Command {
	var (
		outDir     string
		jsonOutput bool
		htmlOutput bool
		jobs       int
		top        int
	)

	cmd := &cobra.Command{
		Use:   "report <capture-dir>",
		Short: "Generate a self-contained incident report",
		Long:  "Combines inspect and triage into a single deliverable: report.json for agents, report.html for operators.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReport(args[0], outDir, jsonOutput, htmlOutput, jobs, top)
		},
	}

	cmd.Flags().StringVar(&outDir, "out", "", "output directory for report artifacts")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output report.json to stdout")
	cmd.Flags().BoolVar(&htmlOutput, "html", true, "include HTML report")
	cmd.Flags().IntVar(&jobs, "jobs", runtime.NumCPU(), "parallel scan workers")
	cmd.Flags().IntVar(&top, "top", 20, "number of top error signatures")

	return cmd
}

func runReport(src, outDir string, jsonOutput, htmlOutput bool, jobs, top int) error {
	cfg := archive.ReportConfig{
		Jobs: jobs,
		Top:  top,
	}

	progress := func(p archive.TriageProgress) {
		if p.Total > 0 {
			pct := float64(p.Scanned) / float64(p.Total) * 100
			fmt.Fprintf(os.Stderr, "\rReport: %s / %s lines (%.1f%%)",
				archive.FormatCount(p.Scanned), archive.FormatCount(p.Total), pct)
		}
	}

	result, err := archive.Report(src, cfg, progress)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\rReport: severity=%s, error_rate=%.1f%%, entries=%s\n",
		result.Severity, result.Triage.ErrorRatePct, archive.FormatCount(result.Capture.Entries))

	if jsonOutput {
		return result.WriteJSON(os.Stdout)
	}

	if outDir == "" {
		return fmt.Errorf("--out is required (or use --json for stdout)")
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Write report.json
	jsonPath := filepath.Join(outDir, "report.json")
	f, err := os.Create(jsonPath)
	if err != nil {
		return fmt.Errorf("create report.json: %w", err)
	}
	if err := result.WriteJSON(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("write report.json: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close report.json: %w", err)
	}

	// Write report.html
	if htmlOutput {
		htmlPath := filepath.Join(outDir, "report.html")
		hf, err := os.Create(htmlPath)
		if err != nil {
			return fmt.Errorf("create report.html: %w", err)
		}
		// Re-run triage for HTML (uses its own SVG renderer)
		triageCfg := archive.TriageConfig{Jobs: jobs, Top: top}
		triageResult, _ := archive.Triage(src, triageCfg, nil)
		meta, _ := recv.ReadMetadata(src)
		if err := result.WriteHTML(hf, triageResult, meta); err != nil {
			_ = hf.Close()
			return fmt.Errorf("write report.html: %w", err)
		}
		if err := hf.Close(); err != nil {
			return fmt.Errorf("close report.html: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "Report: %s\n", filepath.Join(outDir, "report.json"))
	return nil
}
