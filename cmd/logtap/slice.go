package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

func newSliceCmd() *cobra.Command {
	var (
		fromStr string
		toStr   string
		labels  []string
		grepStr string
		outDir  string
	)

	cmd := &cobra.Command{
		Use:   "slice <capture-dir>",
		Short: "Extract a filtered subset of a capture",
		Long:  "Slice a capture directory by time range, labels, or grep into a new, smaller capture directory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSlice(args[0], fromStr, toStr, labels, grepStr, outDir)
		},
	}

	cmd.Flags().StringVar(&fromStr, "from", "", "start time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringVar(&toStr, "to", "", "end time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label filter (key=value, repeatable)")
	cmd.Flags().StringVar(&grepStr, "grep", "", "regex filter on log message")
	cmd.Flags().StringVar(&outDir, "out", "", "output directory (required)")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}

func runSlice(src, fromStr, toStr string, labels []string, grepStr, outDir string) error {
	reader, err := archive.NewReader(src)
	if err != nil {
		return fmt.Errorf("open capture: %w", err)
	}
	meta := reader.Metadata()

	filter, err := buildFilter(fromStr, toStr, labels, grepStr, meta)
	if err != nil {
		return err
	}

	cfg := archive.SliceConfig{
		Compress: true,
	}

	progress := func(p archive.SliceProgress) {
		if p.Total > 0 {
			pct := float64(p.Matched) / float64(p.Total) * 100
			fmt.Fprintf(os.Stderr, "\rSlicing: %s / %s lines (%.1f%%)",
				archive.FormatCount(p.Matched), archive.FormatCount(p.Total), pct)
		} else {
			fmt.Fprintf(os.Stderr, "\rSlicing: %s lines", archive.FormatCount(p.Matched))
		}
	}

	if err := archive.Slice(src, outDir, filter, cfg, progress); err != nil {
		fmt.Fprintln(os.Stderr) // newline after progress
		return err
	}

	// read output metadata for summary
	outMeta, err := archive.Inspect(outDir)
	if err == nil {
		fmt.Fprintf(os.Stderr, "\rSliced: %s lines -> %s (%s)\n",
			archive.FormatCount(outMeta.TotalLines), outDir, archive.FormatBytes(outMeta.DiskSize))
	} else {
		fmt.Fprintln(os.Stderr)
	}

	return nil
}
