package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/ppiankov/logtap/internal/cli"
)

func newSignCmd() *cobra.Command {
	var (
		verify     bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "sign <capture-dir>",
		Short: "Sign or verify capture integrity",
		Long:  "Compute SHA256 hashes of all files in a capture directory and write a manifest. Use --verify to check an existing manifest against current file contents.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSign(args[0], verify, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&verify, "verify", false, "verify existing manifest instead of signing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	addFormatAlias(cmd, &jsonOutput)

	return cmd
}

func runSign(dir string, verify bool, jsonOutput bool) error {
	if verify {
		result, err := archive.Verify(dir)
		if err != nil {
			return fmt.Errorf("sign: %w", err)
		}
		if jsonOutput {
			return result.WriteJSON(os.Stdout)
		}
		result.WriteText(os.Stdout)
		if !result.Valid {
			return cli.NewFindingsError("capture integrity check failed")
		}
		return nil
	}

	result, err := archive.Sign(dir)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	if jsonOutput {
		return result.WriteJSON(os.Stdout)
	}
	result.WriteText(os.Stdout)
	return nil
}
