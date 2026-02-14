package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ppiankov/logtap/internal/forward"
)

func main() {
	target := mustEnv("LOGTAP_TARGET")
	session := mustEnv("LOGTAP_SESSION")
	podName := mustEnv("LOGTAP_POD_NAME")
	namespace := mustEnv("LOGTAP_NAMESPACE")

	fmt.Fprintf(os.Stderr, "logtap-forwarder starting: session=%s target=%s pod=%s/%s\n",
		session, target, namespace, podName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "received %s, shutting down\n", sig)
		cancel()
	}()

	reader, err := forward.NewReader(podName, namespace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init reader: %v\n", err)
		os.Exit(1)
	}

	pusher := forward.NewPusher(target)

	logCh := make(chan forward.LogLine, 1024)

	go func() {
		if err := reader.FollowAll(ctx, logCh); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "follow error: %v\n", err)
		}
	}()

	baseLabels := map[string]string{
		"namespace": namespace,
		"pod":       podName,
		"session":   session,
	}

	const batchSize = 100
	const flushInterval = 500 * time.Millisecond
	batch := make([]forward.TimestampedLine, 0, batchSize)
	currentContainer := ""
	ticker := time.NewTicker(flushInterval)
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
				fmt.Fprintf(os.Stderr, "batch too large, dropping %d lines\n", len(batch))
			} else if ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "push error: %v\n", err)
			}
		}
		batch = batch[:0]
	}

	for {
		select {
		case line, ok := <-logCh:
			if !ok {
				flush()
				return
			}
			if currentContainer != "" && line.Container != currentContainer {
				flush()
			}
			currentContainer = line.Container
			batch = append(batch, forward.TimestampedLine{
				Timestamp: line.Timestamp,
				Line:      line.Line,
			})
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			flush()
			fmt.Fprintln(os.Stderr, "logtap-forwarder stopped")
			return
		}
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "required env var %s not set\n", key)
		os.Exit(1)
	}
	return v
}
