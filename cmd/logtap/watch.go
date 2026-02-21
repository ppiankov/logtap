package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/recv"
)

func newWatchCmd() *cobra.Command {
	var (
		lines      int
		grepStr    string
		labelStr   string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "watch <capture-dir>",
		Short: "Tail a live or completed capture directory",
		Long: `Watch streams new log entries from a capture directory to stdout.
For live captures (receiver still running), it follows new entries like 'tail -f'.
For completed captures, it shows the last N lines and exits.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(args[0], lines, grepStr, labelStr, jsonOutput)
		},
	}

	cmd.Flags().IntVarP(&lines, "lines", "n", 10, "number of initial lines to show")
	cmd.Flags().StringVar(&grepStr, "grep", "", "regex filter on message content")
	cmd.Flags().StringVar(&labelStr, "label", "", "label filter (key=value)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}

func runWatch(dir string, n int, grepStr, labelStr string, jsonOutput bool) error {
	// Validate dir
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("open capture dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	// Build filters
	var grepRe *regexp.Regexp
	if grepStr != "" {
		grepRe, err = regexp.Compile(grepStr)
		if err != nil {
			return fmt.Errorf("compile grep pattern: %w", err)
		}
	}

	var labelKey, labelVal string
	if labelStr != "" {
		idx := strings.IndexByte(labelStr, '=')
		if idx <= 0 {
			return fmt.Errorf("invalid label filter %q (expected key=value)", labelStr)
		}
		labelKey = labelStr[:idx]
		labelVal = labelStr[idx+1:]
	}

	matchEntry := func(e recv.LogEntry) bool {
		if grepRe != nil && !grepRe.MatchString(e.Message) {
			return false
		}
		if labelKey != "" {
			v, ok := e.Labels[labelKey]
			if !ok || v != labelVal {
				return false
			}
		}
		return true
	}

	// Open tailer
	tailer, err := recv.NewTailerFromStart(dir)
	if err != nil {
		return fmt.Errorf("open capture: %w", err)
	}
	defer func() { _ = tailer.Close() }()

	enc := json.NewEncoder(os.Stdout)

	emit := func(e recv.LogEntry) {
		if jsonOutput {
			_ = enc.Encode(e)
		} else {
			printEntry(e)
		}
	}

	// Show last N lines
	last, err := tailer.ReadLast(n)
	if err != nil {
		return fmt.Errorf("read last lines: %w", err)
	}
	for _, e := range last {
		if matchEntry(e) {
			emit(e)
		}
	}

	live := recv.IsLiveCapture(dir)
	if !live {
		return nil
	}

	// Follow mode
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			entries, err := tailer.Tail()
			if err != nil {
				return fmt.Errorf("tail: %w", err)
			}
			for _, e := range entries {
				if matchEntry(e) {
					emit(e)
				}
			}
		}
	}
}

func printEntry(e recv.LogEntry) {
	app := e.Labels["app"]
	if app == "" {
		for _, v := range e.Labels {
			app = v
			break
		}
	}
	_, _ = fmt.Fprintf(os.Stdout, "%s [%s] %s\n",
		e.Timestamp.Format("2006-01-02T15:04:05Z"),
		app,
		e.Message,
	)
}
