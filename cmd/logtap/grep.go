package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

func newGrepCmd() *cobra.Command {
	var (
		fromStr string
		toStr   string
		labels  []string
		count   bool
	)

	cmd := &cobra.Command{
		Use:   "grep <pattern> <capture-dir>",
		Short: "Search capture for matching log entries",
		Long:  "Cross-file regex search across all compressed JSONL files in a capture directory.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGrep(args[0], args[1], fromStr, toStr, labels, count)
		},
	}

	cmd.Flags().StringVar(&fromStr, "from", "", "start time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringVar(&toStr, "to", "", "end time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label filter (key=value, repeatable)")
	cmd.Flags().BoolVar(&count, "count", false, "show match counts per file instead of lines")

	return cmd
}

func runGrep(pattern, src, fromStr, toStr string, labels []string, countMode bool) error {
	reader, err := archive.NewReader(src)
	if err != nil {
		return fmt.Errorf("open capture: %w", err)
	}
	meta := reader.Metadata()

	filter, err := buildFilter(fromStr, toStr, labels, pattern, meta)
	if err != nil {
		return err
	}

	// pattern is required — buildFilter returns nil when no flags set,
	// but we always have a pattern, so filter is never nil here.

	cfg := archive.GrepConfig{
		CountOnly: countMode,
	}

	enc := json.NewEncoder(os.Stdout)

	var totalMatches int64
	onMatch := func(m archive.GrepMatch) {
		_ = enc.Encode(m.Entry)
	}

	progress := func(p archive.GrepProgress) {
		totalMatches = p.Matches
		if p.Total > 0 {
			pct := float64(p.Scanned) / float64(p.Total) * 100
			fmt.Fprintf(os.Stderr, "\rSearching: %s / %s lines (%.1f%%)",
				archive.FormatCount(p.Scanned), archive.FormatCount(p.Total), pct)
		} else {
			fmt.Fprintf(os.Stderr, "\rSearching: %s lines", archive.FormatCount(p.Scanned))
		}
	}

	counts, err := archive.Grep(src, filter, cfg, onMatch, progress)
	if err != nil {
		fmt.Fprintln(os.Stderr)
		return err
	}

	if countMode {
		for _, fc := range counts {
			_, _ = fmt.Fprintf(os.Stdout, "%s\t%d\n", fc.File, fc.Count)
		}
		totalMatches = int64(0)
		for _, fc := range counts {
			totalMatches += fc.Count
		}
	}

	fmt.Fprintf(os.Stderr, "\r%s matches across %d files\n",
		archive.FormatCount(totalMatches), len(counts))

	return nil
}
