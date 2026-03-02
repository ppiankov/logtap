package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/ppiankov/logtap/internal/cli"
)

func newDiffCmd() *cobra.Command {
	var (
		jsonOutput bool
		baseline   bool
		ci         bool
		failOn     []string
	)

	cmd := &cobra.Command{
		Use:   "diff <capture-a> <capture-b>",
		Short: "Compare two capture directories",
		Long: "Compare two captures side-by-side: line counts, labels, error patterns, and per-minute log rates.\n" +
			"With --baseline, treat capture-a as the baseline and produce a verdict.\n" +
			"With --ci, exit code encodes the verdict: 0=pass, 6=fail. Use --fail-on to control which verdicts fail.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ci {
				return runBaselineDiff(args[0], args[1], jsonOutput, true, failOn)
			}
			if baseline {
				return runBaselineDiff(args[0], args[1], jsonOutput, false, nil)
			}
			return runDiff(args[0], args[1], jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&baseline, "baseline", false, "treat first capture as baseline and produce a verdict")
	cmd.Flags().BoolVar(&ci, "ci", false, "CI mode: exit code encodes verdict (0=pass, 6=fail)")
	cmd.Flags().StringSliceVar(&failOn, "fail-on", []string{"regression"}, "verdicts that cause exit 6 in --ci mode")

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

func runBaselineDiff(baselineDir, currentDir string, jsonOutput, ci bool, failOn []string) error {
	result, err := archive.BaselineDiff(baselineDir, currentDir)
	if err != nil {
		return err
	}

	if jsonOutput {
		if err := result.WriteJSON(os.Stdout); err != nil {
			return err
		}
	} else {
		result.WriteText(os.Stdout)
	}

	if ci && verdictFails(result.Verdict, failOn) {
		return cli.NewFindingsError(fmt.Sprintf("verdict: %s", result.Verdict))
	}
	return nil
}

func verdictFails(verdict string, failOn []string) bool {
	for _, v := range failOn {
		if v == verdict {
			return true
		}
	}
	return false
}
