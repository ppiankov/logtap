package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/ppiankov/logtap/internal/recv"
)

func newGrepCmd() *cobra.Command {
	var (
		fromStr    string
		toStr      string
		labels     []string
		count      bool
		sortFlag   bool
		formatFlag string
	)

	cmd := &cobra.Command{
		Use:   "grep <pattern> <capture-dir>",
		Short: "Search capture for matching log entries",
		Long:  "Cross-file regex search across all compressed JSONL files in a capture directory.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pattern, captureDir := args[0], args[1]

			// Detect reversed arguments: if the first arg looks like a directory
			// and the second doesn't exist as a directory, suggest swapping.
			if info, err := os.Stat(pattern); err == nil && info.IsDir() {
				if _, err2 := os.Stat(captureDir); err2 != nil {
					return fmt.Errorf("'%s' is a directory — did you mean: logtap grep %q %s", pattern, captureDir, pattern)
				}
			}

			return runGrep(pattern, captureDir, fromStr, toStr, labels, count, sortFlag, formatFlag)
		},
	}

	cmd.Flags().StringVar(&fromStr, "from", "", "start time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringVar(&toStr, "to", "", "end time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label filter (key=value, repeatable)")
	cmd.Flags().BoolVar(&count, "count", false, "show match counts per file instead of lines")
	cmd.Flags().BoolVar(&sortFlag, "sort", false, "sort results by timestamp (chronological order)")
	cmd.Flags().StringVar(&formatFlag, "format", "json", "output format: json or text (text implies --sort)")

	return cmd
}

func runGrep(pattern, src, fromStr, toStr string, labels []string, countMode, sortByTime bool, format string) error {
	textMode := format == "text"
	if textMode {
		sortByTime = true // text timeline requires chronological order
	}

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

	var collected []recv.LogEntry
	var totalMatches int64
	onMatch := func(m archive.GrepMatch) {
		if sortByTime {
			collected = append(collected, m.Entry)
		} else {
			_ = enc.Encode(m.Entry)
		}
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

	if sortByTime && len(collected) > 0 {
		sort.Slice(collected, func(i, j int) bool {
			return collected[i].Timestamp.Before(collected[j].Timestamp)
		})
		if textMode {
			// Find the longest label value for column alignment
			maxLabel := 0
			for _, e := range collected {
				l := len(entryLabel(e))
				if l > maxLabel {
					maxLabel = l
				}
			}
			for _, e := range collected {
				printTextLine(e, maxLabel)
			}
		} else {
			for _, e := range collected {
				_ = enc.Encode(e)
			}
		}
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

func entryLabel(e recv.LogEntry) string {
	if app := e.Labels["app"]; app != "" {
		return app
	}
	// fall back to first label value
	for _, v := range e.Labels {
		return v
	}
	return "-"
}

func printTextLine(e recv.LogEntry, maxLabel int) {
	ts := e.Timestamp.Format("15:04:05.000")
	label := entryLabel(e)
	pad := ""
	if len(label) < maxLabel {
		pad = strings.Repeat(" ", maxLabel-len(label))
	}
	fmt.Printf("%s [%s]%s %s\n", ts, label, pad, e.Message)
}
