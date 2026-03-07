package main

import "github.com/spf13/cobra"

// addFormatAlias registers a --format flag on cmd that accepts "json" as an
// alias for --json. Commands that already have --format (grep, export) should
// not call this. The jsonOutput pointer must be the same one registered for
// --json on the command.
func addFormatAlias(cmd *cobra.Command, jsonOutput *bool) {
	var format string
	cmd.Flags().StringVar(&format, "format", "", `output format ("json")`)

	orig := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if format == "json" {
			*jsonOutput = true
		}
		if orig != nil {
			return orig(cmd, args)
		}
		return nil
	}
}
