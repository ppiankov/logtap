package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

func newMergeCmd() *cobra.Command {
	var outDir string

	cmd := &cobra.Command{
		Use:   "merge <capture-dir> <capture-dir> [<capture-dir>...] -o <output-dir>",
		Short: "Combine multiple captures into one",
		Long:  "Merge multiple capture directories by timestamp. Copies compressed files without decompressing.",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMerge(args, outDir)
		},
	}

	cmd.Flags().StringVarP(&outDir, "out", "o", "", "output directory (required)")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}

func runMerge(sources []string, outDir string) error {
	progress := func(p archive.MergeProgress) {
		fmt.Fprintf(os.Stderr, "\rMerging: %d / %d files", p.FilesCopied, p.TotalFiles)
	}

	if err := archive.Merge(sources, outDir, progress); err != nil {
		fmt.Fprintln(os.Stderr)
		return err
	}

	// read output metadata for summary
	outMeta, err := archive.Inspect(outDir)
	if err == nil {
		fmt.Fprintf(os.Stderr, "\rMerged: %d sources -> %s (%s, %s)\n",
			len(sources), outDir,
			archive.FormatCount(outMeta.TotalLines)+" lines",
			archive.FormatBytes(outMeta.DiskSize))
	} else {
		fmt.Fprintln(os.Stderr)
	}

	return nil
}
