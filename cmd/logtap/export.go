package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

func newExportCmd() *cobra.Command {
	var (
		formatStr  string
		fromStr    string
		toStr      string
		labels     []string
		grepStr    string
		outPath    string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "export <capture-dir>",
		Short: "Export capture data to parquet, CSV, or JSONL",
		Long:  "Convert capture data to external formats for ingestion into analytics systems (DuckDB, pandas, BigQuery, etc.).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(args[0], formatStr, fromStr, toStr, labels, grepStr, outPath, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&formatStr, "format", "", "output format: parquet, csv, jsonl (required)")
	cmd.Flags().StringVar(&fromStr, "from", "", "start time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringVar(&toStr, "to", "", "end time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label filter (key=value, repeatable)")
	cmd.Flags().StringVar(&grepStr, "grep", "", "regex filter on log message")
	cmd.Flags().StringVar(&outPath, "out", "", "output file path (required)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output summary as JSON")
	_ = cmd.MarkFlagRequired("format")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}

func runExport(src, formatStr, fromStr, toStr string, labels []string, grepStr, outPath string, jsonOutput bool) error {
	format, err := parseExportFormat(formatStr)
	if err != nil {
		return err
	}

	reader, err := archive.NewReader(src)
	if err != nil {
		return fmt.Errorf("open capture: %w", err)
	}
	meta := reader.Metadata()

	filter, err := buildFilter(fromStr, toStr, labels, grepStr, meta)
	if err != nil {
		return err
	}

	progress := func(p archive.ExportProgress) {
		if p.Total > 0 {
			pct := float64(p.Written) / float64(p.Total) * 100
			fmt.Fprintf(os.Stderr, "\rExporting: %s / %s lines (%.1f%%)",
				archive.FormatCount(p.Written), archive.FormatCount(p.Total), pct)
		} else {
			fmt.Fprintf(os.Stderr, "\rExporting: %s lines", archive.FormatCount(p.Written))
		}
	}

	if err := archive.Export(src, outPath, format, filter, progress); err != nil {
		fmt.Fprintln(os.Stderr)
		return err
	}

	info, err := os.Stat(outPath)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr)
		return nil
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"source": src,
			"format": formatStr,
			"output": outPath,
			"lines":  reader.TotalLines(),
			"bytes":  info.Size(),
		})
	}

	_, _ = fmt.Fprintf(os.Stderr, "\rExported: %s lines -> %s (%s)\n",
		archive.FormatCount(reader.TotalLines()), outPath, archive.FormatBytes(info.Size()))
	return nil
}

func parseExportFormat(s string) (archive.ExportFormat, error) {
	switch s {
	case "parquet":
		return archive.FormatParquet, nil
	case "csv":
		return archive.FormatCSV, nil
	case "jsonl":
		return archive.FormatJSONL, nil
	default:
		return "", fmt.Errorf("unsupported format %q: expected parquet, csv, or jsonl", s)
	}
}
