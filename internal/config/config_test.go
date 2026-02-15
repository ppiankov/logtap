package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `recv:
  addr: ":9090"
  dir: "/data/captures"
  disk_cap: "10GB"
  redact: "email,credit_card"
tap:
  namespace: loadtest
  cpu: "50m"
  memory: "32Mi"
defaults:
  timeout: "60s"
  verbose: true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Recv.Addr != ":9090" {
		t.Errorf("Recv.Addr = %q, want %q", cfg.Recv.Addr, ":9090")
	}
	if cfg.Recv.Dir != "/data/captures" {
		t.Errorf("Recv.Dir = %q", cfg.Recv.Dir)
	}
	if cfg.Recv.DiskCap != "10GB" {
		t.Errorf("Recv.DiskCap = %q", cfg.Recv.DiskCap)
	}
	if cfg.Recv.Redact != "email,credit_card" {
		t.Errorf("Recv.Redact = %q", cfg.Recv.Redact)
	}
	if cfg.Tap.Namespace != "loadtest" {
		t.Errorf("Tap.Namespace = %q", cfg.Tap.Namespace)
	}
	if cfg.Tap.CPU != "50m" {
		t.Errorf("Tap.CPU = %q", cfg.Tap.CPU)
	}
	if cfg.Tap.Memory != "32Mi" {
		t.Errorf("Tap.Memory = %q", cfg.Tap.Memory)
	}
	if cfg.Defaults.Timeout != "60s" {
		t.Errorf("Defaults.Timeout = %q", cfg.Defaults.Timeout)
	}
	if !cfg.Defaults.Verbose {
		t.Error("Defaults.Verbose should be true")
	}
}

func TestLoadFromMissingFile(t *testing.T) {
	_, err := LoadFrom("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadReturnsEmptyOnMissingFiles(t *testing.T) {
	// Load() should not error when config files don't exist
	cfg := Load()
	if cfg == nil {
		t.Fatal("Load() returned nil")
	}
	// all fields should be zero values
	if cfg.Recv.Addr != "" {
		t.Errorf("Recv.Addr = %q, want empty", cfg.Recv.Addr)
	}
}

func TestEnvOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `recv:
  addr: ":9090"
  dir: "/from/config"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LOGTAP_RECV_ADDR", ":7070")
	t.Setenv("LOGTAP_RECV_DIR", "/from/env")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Recv.Addr != ":7070" {
		t.Errorf("Recv.Addr = %q, want %q (env override)", cfg.Recv.Addr, ":7070")
	}
	if cfg.Recv.Dir != "/from/env" {
		t.Errorf("Recv.Dir = %q, want %q (env override)", cfg.Recv.Dir, "/from/env")
	}
}

func TestEnvVerbose(t *testing.T) {
	t.Setenv("LOGTAP_VERBOSE", "true")
	cfg := &Config{}
	applyEnv(cfg)
	if !cfg.Defaults.Verbose {
		t.Error("LOGTAP_VERBOSE=true should set Verbose")
	}

	t.Setenv("LOGTAP_VERBOSE", "1")
	cfg = &Config{}
	applyEnv(cfg)
	if !cfg.Defaults.Verbose {
		t.Error("LOGTAP_VERBOSE=1 should set Verbose")
	}

	t.Setenv("LOGTAP_VERBOSE", "false")
	cfg = &Config{}
	applyEnv(cfg)
	if cfg.Defaults.Verbose {
		t.Error("LOGTAP_VERBOSE=false should not set Verbose")
	}
}

func TestAllEnvVars(t *testing.T) {
	t.Setenv("LOGTAP_RECV_ADDR", ":1111")
	t.Setenv("LOGTAP_RECV_DIR", "/env/dir")
	t.Setenv("LOGTAP_RECV_DISK_CAP", "1GB")
	t.Setenv("LOGTAP_RECV_REDACT", "email")
	t.Setenv("LOGTAP_TAP_NAMESPACE", "ns")
	t.Setenv("LOGTAP_TAP_CPU", "100m")
	t.Setenv("LOGTAP_TAP_MEMORY", "64Mi")
	t.Setenv("LOGTAP_TIMEOUT", "120s")
	t.Setenv("LOGTAP_VERBOSE", "true")

	cfg := &Config{}
	applyEnv(cfg)

	if cfg.Recv.Addr != ":1111" {
		t.Errorf("Recv.Addr = %q", cfg.Recv.Addr)
	}
	if cfg.Recv.Dir != "/env/dir" {
		t.Errorf("Recv.Dir = %q", cfg.Recv.Dir)
	}
	if cfg.Recv.DiskCap != "1GB" {
		t.Errorf("Recv.DiskCap = %q", cfg.Recv.DiskCap)
	}
	if cfg.Recv.Redact != "email" {
		t.Errorf("Recv.Redact = %q", cfg.Recv.Redact)
	}
	if cfg.Tap.Namespace != "ns" {
		t.Errorf("Tap.Namespace = %q", cfg.Tap.Namespace)
	}
	if cfg.Tap.CPU != "100m" {
		t.Errorf("Tap.CPU = %q", cfg.Tap.CPU)
	}
	if cfg.Tap.Memory != "64Mi" {
		t.Errorf("Tap.Memory = %q", cfg.Tap.Memory)
	}
	if cfg.Defaults.Timeout != "120s" {
		t.Errorf("Defaults.Timeout = %q", cfg.Defaults.Timeout)
	}
	if !cfg.Defaults.Verbose {
		t.Error("Defaults.Verbose should be true")
	}
}

func TestPartialConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `recv:
  addr: ":8080"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Recv.Addr != ":8080" {
		t.Errorf("Recv.Addr = %q", cfg.Recv.Addr)
	}
	// other fields should be zero
	if cfg.Recv.Dir != "" {
		t.Errorf("Recv.Dir = %q, want empty", cfg.Recv.Dir)
	}
	if cfg.Tap.Namespace != "" {
		t.Errorf("Tap.Namespace = %q, want empty", cfg.Tap.Namespace)
	}
}
