package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/ppiankov/logtap/internal/recv"
)

func newOpenCmd() *cobra.Command {
	var (
		speedStr    string
		fromStr     string
		toStr       string
		labels      []string
		grepStr     string
		injectSpecs []string
		injectAt    string
		injectDur   string
		injectOut   string
		jsonOutput  bool
	)

	cmd := &cobra.Command{
		Use:   "open <capture-dir>",
		Short: "Replay a capture directory",
		Long:  "Open a capture directory written by logtap recv and replay it in a TUI with speed control and filters.\nUse --inject to add synthetic faults to the replay stream.\nUse --inject-out to write the modified stream as a new capture directory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOpen(args[0], speedStr, fromStr, toStr, labels, grepStr,
				injectSpecs, injectAt, injectDur, injectOut, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&speedStr, "speed", "1", "replay speed: 0=instant, 1=realtime, 10=fast-forward (or 10x)")
	cmd.Flags().StringVar(&fromStr, "from", "", "start time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringVar(&toStr, "to", "", "end time filter (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label filter (key=value, repeatable)")
	cmd.Flags().StringVar(&grepStr, "grep", "", "regex filter on log message")
	cmd.Flags().StringArrayVar(&injectSpecs, "inject", nil, "fault to inject (error-spike, service-down=<svc>, latency-spike=<svc>)")
	cmd.Flags().StringVar(&injectAt, "at", "", "injection start time (RFC3339, HH:MM, or -30m)")
	cmd.Flags().StringVar(&injectDur, "duration", "1m", "injection duration (e.g. 30s, 1m, 5m)")
	cmd.Flags().StringVar(&injectOut, "inject-out", "", "write injected stream to new capture directory (skip TUI)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON (with --inject-out)")

	return cmd
}

func runOpen(dir, speedStr, fromStr, toStr string, labels []string, grepStr string,
	injectSpecs []string, injectAt, injectDur, injectOut string, jsonOutput bool) error {

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

	// parse fault injection
	if len(injectSpecs) > 0 {
		faults, err := parseInjectFlags(injectSpecs, injectAt, injectDur, meta)
		if err != nil {
			return err
		}

		if injectOut != "" {
			// output mode — skip TUI, write modified capture
			result, err := archive.InjectWrite(dir, injectOut, filter, faults)
			if err != nil {
				return fmt.Errorf("inject-out: %w", err)
			}
			if jsonOutput {
				return result.WriteJSON(os.Stdout)
			}
			result.WriteText(os.Stdout)
			return nil
		}

		// TUI mode — set transform on feeder after creation
		ring := recv.NewLogRing(0)
		totalLines := reader.TotalLines()
		feeder := archive.NewFeeder(reader, ring, filter, speed)
		feeder.SetTransform(archive.NewInjector(faults))
		model := archive.NewReplayModel(feeder, ring, meta, dir, totalLines)
		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("TUI: %w", err)
		}
		return nil
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

// parseInjectFlags parses --inject, --at, --duration flags into FaultConfigs.
func parseInjectFlags(specs []string, atStr, durStr string, meta *recv.Metadata) ([]archive.FaultConfig, error) {
	refDate := meta.Started
	refTime := meta.Stopped
	if refTime.IsZero() {
		refTime = meta.Started
	}

	at, err := archive.ParseTimeFlag(atStr, refDate, refTime)
	if err != nil {
		return nil, fmt.Errorf("invalid --at: %w", err)
	}
	if at.IsZero() {
		// default to capture start
		at = meta.Started
	}

	dur, err := time.ParseDuration(durStr)
	if err != nil {
		return nil, fmt.Errorf("invalid --duration: %w", err)
	}

	var faults []archive.FaultConfig
	for _, spec := range specs {
		fc, err := archive.ParseFault(spec)
		if err != nil {
			return nil, fmt.Errorf("invalid --inject %q: %w", spec, err)
		}
		fc.At = at
		fc.Duration = dur
		faults = append(faults, fc)
	}
	return faults, nil
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
