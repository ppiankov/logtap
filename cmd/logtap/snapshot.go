package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

func newSnapshotCmd() *cobra.Command {
	var (
		output  string
		extract bool
	)

	cmd := &cobra.Command{
		Use:   "snapshot <capture-dir|archive>",
		Short: "Package or extract a capture archive",
		Long:  "Snapshot creates a single .tar.zst file from a capture directory, or extracts one back to a directory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return fmt.Errorf("--output is required")
			}
			return runSnapshot(args[0], output, extract)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output path (required)")
	cmd.Flags().BoolVar(&extract, "extract", false, "extract archive to directory")

	return cmd
}

func runSnapshot(src, output string, extract bool) error {
	if extract {
		if err := archive.Unpack(src, output); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Extracted to %s\n", output)
		return nil
	}

	if err := archive.Pack(src, output); err != nil {
		return err
	}

	info, err := os.Stat(output)
	if err != nil {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Snapshot saved to %s (%s)\n", output, formatBytes(info.Size()))
	return nil
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
