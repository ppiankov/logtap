package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func newRecvCmd() *cobra.Command {
	var (
		listen         string
		dir            string
		maxFileStr     string
		maxDiskStr     string
		compress       bool
		redactFlag     string
		redactPatterns string
		bufSize        int
		headless       bool
	)

	cmd := &cobra.Command{
		Use:   "recv",
		Short: "Start the log receiver",
		Long:  "Accept Loki push API payloads, optionally redact PII, write compressed JSONL to disk.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecv(listen, dir, maxFileStr, maxDiskStr, compress, redactFlag, redactPatterns, bufSize, headless)
		},
	}

	cmd.Flags().StringVar(&listen, "listen", ":3100", "address to listen on")
	cmd.Flags().StringVar(&dir, "dir", "", "output directory (required)")
	cmd.Flags().StringVar(&maxFileStr, "max-file", "256MB", "max file size before rotation")
	cmd.Flags().StringVar(&maxDiskStr, "max-disk", "50GB", "max total disk usage")
	cmd.Flags().BoolVar(&compress, "compress", true, "zstd compress rotated files")
	cmd.Flags().StringVar(&redactFlag, "redact", "", "enable PII redaction (true or comma-separated pattern names)")
	cmd.Flags().StringVar(&redactPatterns, "redact-patterns", "", "path to custom redaction patterns YAML file")
	cmd.Flags().IntVar(&bufSize, "buffer", 65536, "internal channel buffer size")
	cmd.Flags().BoolVar(&headless, "headless", false, "disable TUI, log to stderr")
	_ = cmd.MarkFlagRequired("dir")

	return cmd
}

func runRecv(listen, dir, maxFileStr, maxDiskStr string, compress bool, redactFlag, redactPatterns string, bufSize int, headless bool) error {
	maxFile, err := parseByteSize(maxFileStr)
	if err != nil {
		return fmt.Errorf("invalid --max-file: %w", err)
	}
	maxDisk, err := parseByteSize(maxDiskStr)
	if err != nil {
		return fmt.Errorf("invalid --max-disk: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// metadata
	meta := &recv.Metadata{
		Version: 1,
		Format:  "jsonl",
		Started: time.Now(),
	}

	// redactor
	var redactor *recv.Redactor
	redactEnabled, redactNames := recv.ParseRedactFlag(redactFlag)
	if redactEnabled {
		redactor, err = recv.NewRedactor(redactNames)
		if err != nil {
			return fmt.Errorf("init redactor: %w", err)
		}
		if redactPatterns != "" {
			if err := redactor.LoadCustomPatterns(redactPatterns); err != nil {
				return fmt.Errorf("load custom patterns: %w", err)
			}
		}
		meta.Redaction = &recv.RedactionInfo{
			Enabled:  true,
			Patterns: redactor.PatternNames(),
		}
	}

	// rotator
	rot, err := rotate.New(rotate.Config{
		Dir:      dir,
		MaxFile:  maxFile,
		MaxDisk:  maxDisk,
		Compress: compress,
	})
	if err != nil {
		return fmt.Errorf("init rotator: %w", err)
	}

	// metrics
	reg := prometheus.DefaultRegisterer
	metrics := recv.NewMetrics(reg)

	// writer
	writer := recv.NewWriter(bufSize, rot, rot.TrackLine)

	// stats and ring (needed by both TUI and server hooks)
	stats := recv.NewStats()
	ring := recv.NewLogRing(0)

	// write initial metadata
	if err := recv.WriteMetadata(dir, meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	// server
	srv := recv.NewServer(listen, writer, redactor, metrics, stats, ring)

	// shutdown performs graceful teardown of all components
	shutdown := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = srv.Shutdown(shutdownCtx)

		writer.Close()
		if err := rot.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "rotator close: %v\n", err)
		}

		meta.Stopped = time.Now()
		meta.TotalLines = writer.LinesWritten()
		meta.TotalBytes = writer.BytesWritten()
		if err := recv.WriteMetadata(dir, meta); err != nil {
			fmt.Fprintf(os.Stderr, "update metadata: %v\n", err)
		}

		metrics.DiskUsage.Set(float64(rot.DiskUsage()))
	}

	// start HTTP server in background
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()

	if headless {
		return runHeadless(listen, dir, writer, errCh, shutdown)
	}
	return runTUI(stats, ring, rot, maxDisk, writer, listen, dir, errCh, shutdown)
}

func runHeadless(listen, dir string, writer *recv.Writer, errCh <-chan error, shutdown func()) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	fmt.Fprintf(os.Stderr, "logtap recv listening on %s, writing to %s\n", listen, dir)

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err.Error() != "http: Server closed" {
			return err
		}
	}

	fmt.Fprintln(os.Stderr, "shutting down...")
	shutdown()
	fmt.Fprintf(os.Stderr, "done: %d lines, %d bytes written\n", writer.LinesWritten(), writer.BytesWritten())
	return nil
}

func runTUI(stats *recv.Stats, ring *recv.LogRing, disk recv.DiskReporter, diskCap int64, writer *recv.Writer, listen, dir string, errCh <-chan error, shutdown func()) error {
	model := recv.NewTUIModel(stats, ring, disk, diskCap, writer, listen, dir)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// forward server errors to TUI quit
	go func() {
		if err := <-errCh; err != nil {
			if err.Error() != "http: Server closed" {
				p.Quit()
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI: %w", err)
	}

	shutdown()
	return nil
}

var byteSizePattern = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*(KB|MB|GB|TB|B)?$`)

func parseByteSize(s string) (int64, error) {
	m := byteSizePattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("invalid size: %q", s)
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, err
	}
	unit := strings.ToUpper(m[2])
	switch unit {
	case "TB":
		val *= 1 << 40
	case "GB":
		val *= 1 << 30
	case "MB":
		val *= 1 << 20
	case "KB":
		val *= 1 << 10
	case "B", "":
		// bytes
	}
	return int64(val), nil
}
