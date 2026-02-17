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

	"github.com/ppiankov/logtap/internal/k8s"
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
		tlsCert        string
		tlsKey         string
		inCluster      bool
		image          string
		namespace      string
		ttlStr         string
		webhookURLs    []string
		webhookEvents  string
		alertRulesPath string
	)

	cmd := &cobra.Command{
		Use:   "recv",
		Short: "Start the log receiver",
		Long:  "Accept Loki push API payloads, optionally redact PII, write compressed JSONL to disk.",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			applyConfigDefaults(cmd)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if inCluster {
				if image == "" {
					return fmt.Errorf("--image required with --in-cluster")
				}
				var ttl time.Duration
				if ttlStr != "" {
					var err error
					ttl, err = time.ParseDuration(ttlStr)
					if err != nil {
						return fmt.Errorf("invalid --ttl: %w", err)
					}
				}
				return runRecvInCluster(inClusterOpts{
					image:      image,
					namespace:  namespace,
					maxFile:    maxFileStr,
					maxDisk:    maxDiskStr,
					compress:   compress,
					redact:     redactFlag,
					listenPort: 9000,
					ttl:        ttl,
				})
			}
			if dir == "" {
				return fmt.Errorf("--dir is required (or use --in-cluster)")
			}
			return runRecv(listen, dir, maxFileStr, maxDiskStr, compress, redactFlag, redactPatterns, bufSize, headless, tlsCert, tlsKey, webhookURLs, webhookEvents, alertRulesPath)
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
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "TLS certificate file")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "TLS key file")
	cmd.Flags().BoolVar(&inCluster, "in-cluster", false, "deploy receiver as in-cluster pod")
	cmd.Flags().StringVar(&image, "image", "", "container image for in-cluster receiver (required with --in-cluster)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "logtap", "namespace for in-cluster resources")
	cmd.Flags().StringVar(&ttlStr, "ttl", "4h", "receiver pod TTL for in-cluster mode (e.g. 4h, 30m)")
	cmd.Flags().StringSliceVar(&webhookURLs, "webhook", nil, "webhook URLs to notify on lifecycle events (repeatable)")
	cmd.Flags().StringVar(&webhookEvents, "webhook-events", "", "comma-separated event filter (start,stop,rotation,error,disk-warning)")
	cmd.Flags().StringVar(&alertRulesPath, "alert-rules", "", "path to alert rules YAML file")

	return cmd
}

func runRecv(listen, dir, maxFileStr, maxDiskStr string, compress bool, redactFlag, redactPatterns string, bufSize int, headless bool, tlsCert, tlsKey string, webhookURLs []string, webhookEvents string, alertRulesPath string) error {
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
	var redactInfo string
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
		redactInfo = fmt.Sprintf("on (%d patterns)", len(redactor.PatternNames()))
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

	// webhook dispatcher — merge config URLs if CLI provided none
	if len(webhookURLs) == 0 && cfg != nil && len(cfg.Recv.Webhooks) > 0 {
		webhookURLs = cfg.Recv.Webhooks
	}
	var eventFilter []string
	if webhookEvents != "" {
		eventFilter = strings.Split(webhookEvents, ",")
	}
	dispatcher := recv.NewWebhookDispatcher(webhookURLs, eventFilter)

	// metrics
	reg := prometheus.DefaultRegisterer
	metrics := recv.NewMetrics(reg)

	// wire redaction hit counts to metrics
	if redactor != nil {
		redactor.SetOnRedact(func(pattern string) {
			metrics.RedactionsTotal.WithLabelValues(pattern).Inc()
		})
	}

	// writer
	writer := recv.NewWriter(bufSize, rot, rot.TrackLine)
	writer.SetQueueGauge(func(v float64) { metrics.WriterQueueLength.Set(v) })

	// rotation metrics + webhook notifications
	rot.SetOnRotate(func(reason string) {
		metrics.RotationTotal.WithLabelValues(reason).Inc()
		dispatcher.Fire(recv.WebhookEvent{Event: "rotation", Detail: reason})
	})
	rot.SetOnError(func() {
		metrics.RotationErrors.Inc()
		dispatcher.Fire(recv.WebhookEvent{Event: "error"})
	})
	rot.SetOnDiskWarning(func(usage, cap int64) {
		dispatcher.Fire(recv.WebhookEvent{
			Event: "disk-warning",
			Dir:   dir,
			Stats: &recv.WebhookStats{DiskUsage: usage, DiskCap: cap},
		})
	})

	// stats and ring (needed by both TUI and server hooks)
	stats := recv.NewStats()
	ring := recv.NewLogRing(0)

	// alert engine
	var alertEngine *recv.AlertEngine
	if alertRulesPath != "" {
		alertRules, err := recv.LoadAlertRules(alertRulesPath)
		if err != nil {
			return fmt.Errorf("load alert rules: %w", err)
		}
		alertEngine = recv.NewAlertEngine(alertRules, dispatcher)
	}

	// write initial metadata
	if err := recv.WriteMetadata(dir, meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	// audit logger
	audit, err := recv.NewAuditLogger(dir)
	if err != nil {
		return fmt.Errorf("init audit logger: %w", err)
	}

	// server
	srv := recv.NewServer(listen, writer, redactor, metrics, stats, ring)
	srv.SetVersion(version)
	srv.SetAuditLogger(audit)

	audit.Log(recv.AuditEntry{Event: "server_started"})
	dispatcher.Fire(recv.WebhookEvent{Event: "start", Dir: dir})

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

		audit.Log(recv.AuditEntry{Event: "server_stopped"})
		_ = audit.Close()

		dispatcher.Fire(recv.WebhookEvent{
			Event: "stop",
			Dir:   dir,
			Stats: &recv.WebhookStats{
				LinesWritten: writer.LinesWritten(),
				BytesWritten: writer.BytesWritten(),
				DiskUsage:    rot.DiskUsage(),
				DiskCap:      maxDisk,
			},
		})

		metrics.DiskUsage.Set(float64(rot.DiskUsage()))
	}

	// alert evaluation loop
	if alertEngine != nil {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				var diskUsage int64
				if rot != nil {
					diskUsage = rot.DiskUsage()
				}
				snap := stats.Snapshot(diskUsage, maxDisk, writer.BytesWritten())
				alertEngine.Evaluate(snap)
			}
		}()
	}

	// start HTTP server in background
	errCh := make(chan error, 1)
	go func() {
		var srvErr error
		if tlsCert != "" && tlsKey != "" {
			srvErr = srv.ListenAndServeTLS(tlsCert, tlsKey)
		} else {
			srvErr = srv.ListenAndServe()
		}
		if srvErr != nil {
			errCh <- srvErr
		}
	}()

	if headless {
		return runHeadless(listen, dir, writer, errCh, shutdown)
	}
	return runTUI(stats, ring, rot, maxDisk, writer, listen, dir, redactInfo, errCh, shutdown)
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

func runTUI(stats *recv.Stats, ring *recv.LogRing, disk recv.DiskReporter, diskCap int64, writer *recv.Writer, listen, dir, redactInfo string, errCh <-chan error, shutdown func()) error {
	model := recv.NewTUIModel(stats, ring, disk, diskCap, writer, listen, dir, redactInfo)
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

type inClusterOpts struct {
	image      string
	namespace  string
	maxFile    string
	maxDisk    string
	compress   bool
	redact     string
	listenPort int
	ttl        time.Duration
}

func runRecvInCluster(opts inClusterOpts) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	c, err := k8s.NewClient(opts.namespace)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	labels := map[string]string{
		k8s.LabelManagedBy: k8s.ManagedByValue,
		k8s.LabelName:      k8s.ReceiverName,
	}

	podArgs := []string{
		"recv", "--headless",
		"--listen", fmt.Sprintf(":%d", opts.listenPort),
		"--dir", "/data",
		"--max-file", opts.maxFile,
		"--max-disk", opts.maxDisk,
	}
	if !opts.compress {
		podArgs = append(podArgs, "--compress=false")
	}
	if opts.redact != "" {
		podArgs = append(podArgs, "--redact", opts.redact)
	}

	spec := k8s.ReceiverSpec{
		Image:     opts.image,
		Namespace: c.NS,
		PodName:   k8s.ReceiverName,
		SvcName:   k8s.ReceiverName,
		Port:      int32(opts.listenPort),
		Args:      podArgs,
		Labels:    labels,
		TTL:       opts.ttl,
	}

	fmt.Fprintf(os.Stderr, "deploying receiver pod in %s...\n", c.NS)
	res, err := k8s.DeployReceiver(ctx, c, spec)
	if err != nil {
		if res != nil {
			_ = k8s.DeleteReceiver(context.Background(), c, res)
		}
		return fmt.Errorf("deploy receiver: %w", err)
	}
	defer func() {
		fmt.Fprintln(os.Stderr, "cleaning up cluster resources...")
		if err := k8s.DeleteReceiver(context.Background(), c, res); err != nil {
			fmt.Fprintf(os.Stderr, "cleanup error: %v\n", err)
		}
	}()

	fmt.Fprintln(os.Stderr, "waiting for pod ready...")
	if err := k8s.WaitForPodReady(ctx, c, c.NS, k8s.ReceiverName, 60*time.Second); err != nil {
		return fmt.Errorf("pod not ready: %w", err)
	}

	pfSpec := k8s.PortForwardSpec{
		Namespace:  c.NS,
		PodName:    k8s.ReceiverName,
		RemotePort: opts.listenPort,
		LocalPort:  0,
	}

	tunnel, err := k8s.NewPortForwardTunnel(c.RestConfig, c.CS, pfSpec, os.Stderr, os.Stderr)
	if err != nil {
		return fmt.Errorf("create port-forward: %w", err)
	}

	tunnelErrCh := make(chan error, 1)
	go func() {
		tunnelErrCh <- tunnel.Run()
	}()

	select {
	case <-tunnel.ReadyCh():
	case err := <-tunnelErrCh:
		return fmt.Errorf("port-forward failed: %w", err)
	case <-ctx.Done():
		tunnel.Stop()
		return ctx.Err()
	}

	localPort, err := tunnel.GetLocalPort()
	if err != nil {
		tunnel.Stop()
		return fmt.Errorf("get local port: %w", err)
	}

	fmt.Fprintf(os.Stderr, "receiver ready — push logs to http://localhost:%d/loki/api/v1/push\n", localPort)
	fmt.Fprintf(os.Stderr, "in-cluster service: %s.%s:%d\n", k8s.ReceiverName, c.NS, opts.listenPort)
	fmt.Fprintln(os.Stderr, "press Ctrl+C to stop and clean up")

	select {
	case <-ctx.Done():
	case err := <-tunnelErrCh:
		if err != nil {
			fmt.Fprintf(os.Stderr, "port-forward error: %v\n", err)
		}
	}

	tunnel.Stop()
	fmt.Fprintln(os.Stderr, "shutting down...")
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
