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
		outDir     string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "merge <capture-dir> <capture-dir> [<capture-dir>...] -o <output-dir>",
		Short: "Combine multiple captures into one",
		Long:  "Merge multiple capture directories by timestamp. Copies compressed files without decompressing.",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMerge(args, outDir, jsonOutput)
		},
	}

	cmd.Flags().StringVarP(&outDir, "out", "o", "", "output directory (required)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output summary as JSON")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}

func runMerge(sources []string, outDir string, jsonOutput bool) error {
	progress := func(p archive.MergeProgress) {
		_, _ = fmt.Fprintf(os.Stderr, "\rMerging: %d / %d files", p.FilesCopied, p.TotalFiles)
	}

	if err := archive.Merge(sources, outDir, progress); err != nil {
		_, _ = fmt.Fprintln(os.Stderr)
		return err
	}

	outMeta, err := archive.Inspect(outDir)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr)
		return nil
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"sources": sources,
			"output":  outDir,
			"entries": outMeta.TotalLines,
			"files":   outMeta.Files,
			"bytes":   outMeta.DiskSize,
		})
	}

	_, _ = fmt.Fprintf(os.Stderr, "\rMerged: %d sources -> %s (%s, %s)\n",
		len(sources), outDir,
		archive.FormatCount(outMeta.TotalLines)+" lines",
		archive.FormatBytes(outMeta.DiskSize))
	return nil
}
