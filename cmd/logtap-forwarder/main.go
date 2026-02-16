package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ppiankov/logtap/internal/forward"
)

const (
	envTarget    = "LOGTAP_TARGET"
	envSession   = "LOGTAP_SESSION"
	envPodName   = "LOGTAP_POD_NAME"
	envNamespace = "LOGTAP_NAMESPACE"

	defaultHealthAddr    = ":9091"
	defaultBatchSize     = 100
	defaultFlushInterval = 500 * time.Millisecond
)

type Config struct {
	Target     string
	Session    string
	PodName    string
	Namespace  string
	HealthAddr string
}

type logReader interface {
	FollowAll(ctx context.Context, out chan<- forward.LogLine) error
}

type logPusher interface {
	Push(ctx context.Context, labels map[string]string, lines []forward.TimestampedLine) error
}

type Dependencies struct {
	NewReader func(podName, namespace string) (logReader, error)
	NewPusher func(target string) logPusher
	LogWriter io.Writer
}

func main() {
	cfg, err := loadConfigFromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "logtap-forwarder starting: session=%s target=%s pod=%s/%s\n",
		cfg.Session, cfg.Target, cfg.Namespace, cfg.PodName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "received %s, shutting down\n", sig)
		cancel()
	}()

	if _, err := startHealthServer(ctx, cfg.HealthAddr, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "health server: %v\n", err)
	}

	if err := run(ctx, cfg, Dependencies{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		Target:     getenv(envTarget),
		Session:    getenv(envSession),
		PodName:    getenv(envPodName),
		Namespace:  getenv(envNamespace),
		HealthAddr: defaultHealthAddr,
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateConfig(cfg Config) error {
	if cfg.Target == "" {
		return fmt.Errorf("required env var %s not set", envTarget)
	}
	if cfg.Session == "" {
		return fmt.Errorf("required env var %s not set", envSession)
	}
	if cfg.PodName == "" {
		return fmt.Errorf("required env var %s not set", envPodName)
	}
	if cfg.Namespace == "" {
		return fmt.Errorf("required env var %s not set", envNamespace)
	}
	return nil
}

func healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

func startHealthServer(ctx context.Context, addr string, log io.Writer) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	return startHealthServerWithListener(ctx, ln, log)
}

func startHealthServerWithListener(ctx context.Context, ln net.Listener, log io.Writer) (string, error) {
	srv := &http.Server{Handler: healthHandler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			_, _ = fmt.Fprintf(log, "health server: %v\n", err)
		}
	}()

	return ln.Addr().String(), nil
}

func run(ctx context.Context, cfg Config, deps Dependencies) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}
	if deps.NewReader == nil {
		deps.NewReader = func(podName, namespace string) (logReader, error) {
			return forward.NewReader(podName, namespace)
		}
	}
	if deps.NewPusher == nil {
		deps.NewPusher = func(target string) logPusher {
			return forward.NewPusher(target)
		}
	}
	if deps.LogWriter == nil {
		deps.LogWriter = os.Stderr
	}

	reader, err := deps.NewReader(cfg.PodName, cfg.Namespace)
	if err != nil {
		return fmt.Errorf("init reader: %w", err)
	}

	pusher := deps.NewPusher(cfg.Target)

	logCh := make(chan forward.LogLine, 1024)

	go func() {
		if err := reader.FollowAll(ctx, logCh); err != nil && ctx.Err() == nil {
			_, _ = fmt.Fprintf(deps.LogWriter, "follow error: %v\n", err)
		}
	}()

	baseLabels := map[string]string{
		"namespace": cfg.Namespace,
		"pod":       cfg.PodName,
		"session":   cfg.Session,
	}

	batch := make([]forward.TimestampedLine, 0, defaultBatchSize)
	currentContainer := ""
	ticker := time.NewTicker(defaultFlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		labels := make(map[string]string, len(baseLabels)+1)
		for k, v := range baseLabels {
			labels[k] = v
		}
		labels["container"] = currentContainer

		if err := pusher.Push(ctx, labels, batch); err != nil {
			if err == forward.ErrBufferExceeded {
				_, _ = fmt.Fprintf(deps.LogWriter, "batch too large, dropping %d lines\n", len(batch))
			} else if ctx.Err() == nil {
				_, _ = fmt.Fprintf(deps.LogWriter, "push error: %v\n", err)
			}
		}
		batch = batch[:0]
	}

	for {
		select {
		case line, ok := <-logCh:
			if !ok {
				flush()
				return nil
			}
			if currentContainer != "" && line.Container != currentContainer {
				flush()
			}
			currentContainer = line.Container
			batch = append(batch, forward.TimestampedLine{
				Timestamp: line.Timestamp,
				Line:      line.Line,
			})
			if len(batch) >= defaultBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			flush()
			_, _ = fmt.Fprintln(deps.LogWriter, "logtap-forwarder stopped")
			return nil
		}
	}
}
