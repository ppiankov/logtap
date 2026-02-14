package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

func newInspectCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "inspect <capture-dir>",
		Short: "Show capture directory summary",
		Long:  "Read metadata.json and index.jsonl from a capture directory and display label breakdown, timeline, and size stats. No decompression — instant even for large captures.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspect(args[0], jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}

func runInspect(dir string, jsonOutput bool) error {
	summary, err := archive.Inspect(dir)
	if err != nil {
		return fmt.Errorf("inspect: %w", err)
	}

	if jsonOutput {
		return summary.WriteJSON(os.Stdout)
	}

	summary.WriteText(os.Stdout)
	return nil
}
