package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
)

var (
	sliceFrom  string
	sliceTo    string
	sliceLabel []string
	sliceGrep  string
	sliceOut   string
	sliceJSON  bool
)

func newSliceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "slice <capture-directory>",
		Short: "Extract a time range and/or label filter into a new smaller capture directory",
		Long:  "Slice reads a capture directory, applies time range and/or label filters, and writes matching entries to a new capture directory with its own metadata and index.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			captureDir := args[0]

			if sliceOut == "" {
				return fmt.Errorf("--out flag is required")
			}

			var fromTime, toTime time.Time
			var err error

			if sliceFrom != "" {
				fromTime, err = parseTime(sliceFrom)
				if err != nil {
					return fmt.Errorf("invalid --from: %w", err)
				}
			}
			if sliceTo != "" {
				toTime, err = parseTime(sliceTo)
				if err != nil {
					return fmt.Errorf("invalid --to: %w", err)
				}
			}

			var labelFilters []archive.LabelFilter
			for _, l := range sliceLabel {
				parts := strings.SplitN(l, "=", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
					return fmt.Errorf("invalid --label format '%s': expected key=value", l)
				}
				labelFilters = append(labelFilters, archive.LabelFilter{Key: parts[0], Value: parts[1]})
			}

			var grepRegex *regexp.Regexp
			if sliceGrep != "" {
				grepRegex, err = regexp.Compile(sliceGrep)
				if err != nil {
					return fmt.Errorf("invalid --grep regex: %w", err)
				}
			}

			opts := archive.SliceOptions{
				CaptureDir: captureDir,
				OutputDir:  sliceOut,
				From:       fromTime,
				To:         toTime,
				Labels:     labelFilters,
				Grep:       grepRegex,
			}

			if err := archive.Slice(opts); err != nil {
				return err
			}

			if sliceJSON {
				summary, err := archive.Inspect(sliceOut)
				if err != nil {
					return err
				}
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"source":  captureDir,
					"output":  sliceOut,
					"entries": summary.TotalLines,
					"files":   summary.Files,
					"bytes":   summary.DiskSize,
				})
			}

			_, _ = fmt.Fprintln(os.Stderr, "Slicing complete.")
			return nil
		},
	}

	cmd.Flags().StringVar(&sliceFrom, "from", "", "start time (absolute or relative: 10:32, 2024-01-15T10:32:00Z, -30m)")
	cmd.Flags().StringVar(&sliceTo, "to", "", "end time (same formats as --from)")
	cmd.Flags().StringArrayVar(&sliceLabel, "label", []string{}, "label filter (key=value), repeatable")
	cmd.Flags().StringVar(&sliceGrep, "grep", "", "regex filter on message content")
	cmd.Flags().StringVarP(&sliceOut, "out", "o", "", "output directory for the new capture (required)")
	cmd.Flags().BoolVar(&sliceJSON, "json", false, "output summary as JSON")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}

// runSlice is the testable entry point for the slice command.
func runSlice(src, fromStr, toStr string, labels []string, grepStr, outDir string) error {
	now := time.Now()
	var fromTime, toTime time.Time
	var err error

	if fromStr != "" {
		fromTime, err = archive.ParseTimeFlag(fromStr, now, now)
		if err != nil {
			return fmt.Errorf("invalid --from: %w", err)
		}
	}
	if toStr != "" {
		toTime, err = archive.ParseTimeFlag(toStr, now, now)
		if err != nil {
			return fmt.Errorf("invalid --to: %w", err)
		}
	}

	var labelFilters []archive.LabelFilter
	for _, l := range labels {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			return fmt.Errorf("invalid label %q: expected key=value", l)
		}
		labelFilters = append(labelFilters, archive.LabelFilter{Key: parts[0], Value: parts[1]})
	}

	var grepRegex *regexp.Regexp
	if grepStr != "" {
		grepRegex, err = regexp.Compile(grepStr)
		if err != nil {
			return fmt.Errorf("invalid grep: %w", err)
		}
	}

	return archive.Slice(archive.SliceOptions{
		CaptureDir: src,
		OutputDir:  outDir,
		From:       fromTime,
		To:         toTime,
		Labels:     labelFilters,
		Grep:       grepRegex,
	})
}

// parseTime attempts to parse a string into a time.Time, supporting RFC3339, 15:04 (HH:MM), or duration relative to now.
func parseTime(s string) (time.Time, error) {
	// Try RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}

	// Try HH:MM format (today's date)
	if t, err := time.Parse("15:04", s); err == nil {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), now.Location()), nil
	}

	// Try duration (e.g., -30m, 1h)
	if strings.HasPrefix(s, "-") || strings.HasSuffix(s, "m") || strings.HasSuffix(s, "h") { // simplified check for duration
		d, err := time.ParseDuration(strings.TrimPrefix(s, "-"))
		if err == nil {
			return time.Now().Add(-d), nil // relative to now, e.g., "30m" -> 30 mins ago
		}
	}

	return time.Time{}, fmt.Errorf("unsupported time format: %s", s)
}
