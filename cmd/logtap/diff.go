package main

import (
	"os"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	var jsonOutput bool
	var baseline bool

	cmd := &cobra.Command{
		Use:   "diff <capture-a> <capture-b>",
		Short: "Compare two capture directories",
		Long:  "Compare two captures side-by-side: line counts, labels, error patterns, and per-minute log rates.\nWith --baseline, treat capture-a as the baseline and produce a verdict.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseline {
				return runBaselineDiff(args[0], args[1], jsonOutput)
			}
			return runDiff(args[0], args[1], jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&baseline, "baseline", false, "treat first capture as baseline and produce a verdict")

	return cmd
}

func runDiff(dirA, dirB string, jsonOutput bool) error {
	result, err := archive.Diff(dirA, dirB)
	if err != nil {
		return err
	}

	if jsonOutput {
		return result.WriteJSON(os.Stdout)
	}

	result.WriteText(os.Stdout)
	return nil
}

func runBaselineDiff(baselineDir, currentDir string, jsonOutput bool) error {
	result, err := archive.BaselineDiff(baselineDir, currentDir)
	if err != nil {
		return err
	}

	if jsonOutput {
		return result.WriteJSON(os.Stdout)
	}

	result.WriteText(os.Stdout)
	return nil
}
