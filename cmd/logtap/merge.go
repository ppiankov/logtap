package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

func newMergeCmd() *cobra.Command {
	var (
		outDir       string
		jsonOutput   bool
		clockCorrect bool
	)

	cmd := &cobra.Command{
		Use:   "merge <capture-dir> <capture-dir> [<capture-dir>...] -o <output-dir>",
		Short: "Combine multiple captures into one",
		Long: "Merge multiple capture directories by timestamp. Copies compressed files without decompressing.\n" +
			"With --clock-correct, detects and corrects clock skew between sources.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMerge(args, outDir, jsonOutput, clockCorrect)
		},
	}

	cmd.Flags().StringVarP(&outDir, "out", "o", "", "output directory (required)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output summary as JSON")
	addFormatAlias(cmd, &jsonOutput)
	cmd.Flags().BoolVar(&clockCorrect, "clock-correct", false, "detect and correct clock skew between sources")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}

func runMerge(sources []string, outDir string, jsonOutput, clockCorrect bool) error {
	progress := func(p archive.MergeProgress) {
		_, _ = fmt.Fprintf(os.Stderr, "\rMerging: %d / %d files", p.FilesCopied, p.TotalFiles)
	}

	var corrections []archive.ClockCorrection

	if clockCorrect {
		var err error
		corrections, err = archive.MergeWithCorrection(sources, outDir, progress)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr)
			return err
		}
	} else {
		if err := archive.Merge(sources, outDir, progress); err != nil {
			_, _ = fmt.Fprintln(os.Stderr)
			return err
		}
	}

	outMeta, err := archive.Inspect(outDir)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr)
		return nil
	}

	if jsonOutput {
		result := map[string]any{
			"sources": sources,
			"output":  outDir,
			"entries": outMeta.TotalLines,
			"files":   outMeta.Files,
			"bytes":   outMeta.DiskSize,
		}
		if len(corrections) > 0 {
			result["clock_corrections"] = corrections
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}

	_, _ = fmt.Fprintf(os.Stderr, "\rMerged: %d sources -> %s (%s, %s)\n",
		len(sources), outDir,
		archive.FormatCount(outMeta.TotalLines)+" lines",
		archive.FormatBytes(outMeta.DiskSize))

	if len(corrections) > 0 {
		for _, cc := range corrections {
			_, _ = fmt.Fprintf(os.Stderr, "  Clock correction: %s offset=%dms confidence=%.2f method=%s\n",
				cc.Source, cc.OffsetMs, cc.Confidence, cc.Method)
		}
	}

	return nil
}
