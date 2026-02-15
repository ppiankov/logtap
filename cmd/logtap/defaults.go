package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"
)

// clusterContext returns a context with the configured timeout for cluster operations.
// The caller must call cancel when done.
func clusterContext() (context.Context, context.CancelFunc) {
	timeout := defaultTimeout

	// Flag overrides config
	if timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = d
		}
	} else if cfg != nil && cfg.Defaults.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Defaults.Timeout); err == nil {
			timeout = d
		}
	}

	return context.WithTimeout(context.Background(), timeout)
}

// applyConfigDefaults sets flag values from config when the flag
// was not explicitly set on the command line. Flags > env > config > defaults.
// The config package already handles env > config, so we just need to
// check if the flag was changed and apply config if not.
func applyConfigDefaults(cmd *cobra.Command) {
	if cfg == nil {
		return
	}

	setDefault := func(name, value string) {
		if value != "" && !cmd.Flags().Changed(name) {
			if f := cmd.Flags().Lookup(name); f != nil {
				_ = f.Value.Set(value)
			}
		}
	}

	// recv defaults
	setDefault("listen", cfg.Recv.Addr)
	setDefault("dir", cfg.Recv.Dir)
	setDefault("max-disk", cfg.Recv.DiskCap)
	setDefault("redact", cfg.Recv.Redact)
	setDefault("redact-patterns", cfg.Recv.RedactPatterns)

	// tap defaults
	setDefault("namespace", cfg.Tap.Namespace)
	setDefault("cpu", cfg.Tap.CPU)
	setDefault("memory", cfg.Tap.Memory)
}
