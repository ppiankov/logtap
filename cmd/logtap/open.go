package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/ppiankov/logtap/internal/recv"
)

func newOpenCmd() *cobra.Command {
	var (
		speedStr string
		fromStr  string
		toStr    string
		labels   []string
		grepStr  string
	)

	cmd := &cobra.Command{
		Use:   "open <capture-dir>",
		Short: "Replay a capture directory",
		Long:  "Open a capture directory written by logtap recv and replay it in a TUI with speed control and filters.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOpen(args[0], speedStr, fromStr, toStr, labels, grepStr)
		},
	}

	cmd.Flags().StringVar(&speedStr, "speed", "1", "replay speed: 0=instant, 1=realtime, 10=fast-forward (or 10x)")
	cmd.Flags().StringVar(&fromStr, "from", "", "start time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringVar(&toStr, "to", "", "end time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label filter (key=value, repeatable)")
	cmd.Flags().StringVar(&grepStr, "grep", "", "regex filter on log message")

	return cmd
}

func runOpen(dir, speedStr, fromStr, toStr string, labels []string, grepStr string) error {
	reader, err := archive.NewReader(dir)
	if err != nil {
		return fmt.Errorf("open capture: %w", err)
	}
	meta := reader.Metadata()

	// parse speed
	speed, err := parseSpeed(speedStr)
	if err != nil {
		return fmt.Errorf("invalid --speed: %w", err)
	}

	// parse filters
	filter, err := buildFilter(fromStr, toStr, labels, grepStr, meta)
	if err != nil {
		return err
	}

	ring := recv.NewLogRing(0)
	totalLines := reader.TotalLines()
	feeder := archive.NewFeeder(reader, ring, filter, speed)
	model := archive.NewReplayModel(feeder, ring, meta, dir, totalLines)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI: %w", err)
	}
	return nil
}

func parseSpeed(s string) (archive.Speed, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "x")
	var val float64
	if _, err := fmt.Sscanf(s, "%f", &val); err != nil {
		return 0, fmt.Errorf("invalid speed %q", s)
	}
	if val < 0 {
		return 0, fmt.Errorf("speed must be >= 0")
	}
	return archive.Speed(val), nil
}
