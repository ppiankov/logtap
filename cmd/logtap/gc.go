package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

func newGCCmd() *cobra.Command {
	var maxAgeStr string
	var maxTotalStr string
	var dryRun bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "gc <captures-dir>",
		Short: "Delete old capture directories",
		Long:  "Delete capture subdirectories based on age or total disk usage.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGC(args[0], maxAgeStr, maxTotalStr, dryRun, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&maxAgeStr, "max-age", "", "delete captures older than this (e.g. 7d, 24h)")
	cmd.Flags().StringVar(&maxTotalStr, "max-total", "", "delete oldest captures until total size under limit (e.g. 100GB)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be deleted without removing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output deletion list as JSON")

	return cmd
}

func runGC(dir, maxAgeStr, maxTotalStr string, dryRun, jsonOutput bool) error {
	if maxAgeStr == "" && maxTotalStr == "" {
		return fmt.Errorf("--max-age or --max-total is required")
	}

	var maxAge time.Duration
	if maxAgeStr != "" {
		var err error
		maxAge, err = parseGCAge(maxAgeStr)
		if err != nil {
			return fmt.Errorf("invalid --max-age: %w", err)
		}
		if maxAge <= 0 {
			return fmt.Errorf("invalid --max-age: must be positive")
		}
	}

	var maxTotal int64
	if maxTotalStr != "" {
		var err error
		maxTotal, err = parseByteSize(maxTotalStr)
		if err != nil {
			return fmt.Errorf("invalid --max-total: %w", err)
		}
		if maxTotal <= 0 {
			return fmt.Errorf("invalid --max-total: must be positive")
		}
	}

	result, err := archive.GC(dir, archive.GCOptions{
		MaxAge:        maxAge,
		MaxTotalBytes: maxTotal,
		DryRun:        dryRun,
		Now:           time.Now(),
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		return result.WriteJSON(os.Stdout)
	}

	result.WriteText(os.Stdout)
	return nil
}

func parseGCAge(input string) (time.Duration, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, "d") {
		valStr := strings.TrimSuffix(lower, "d")
		if valStr == "" {
			return 0, fmt.Errorf("invalid day duration: %q", input)
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(val * 24 * float64(time.Hour)), nil
	}
	return time.ParseDuration(s)
}
