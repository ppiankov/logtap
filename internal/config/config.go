package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds persistent defaults loaded from config files.
type Config struct {
	Recv     RecvConfig     `yaml:"recv"`
	Tap      TapConfig      `yaml:"tap"`
	Defaults DefaultsConfig `yaml:"defaults"`
}

// RecvConfig holds receiver defaults.
type RecvConfig struct {
	Addr           string   `yaml:"addr"`
	Dir            string   `yaml:"dir"`
	DiskCap        string   `yaml:"disk_cap"`
	Redact         string   `yaml:"redact"`
	RedactPatterns string   `yaml:"redact_patterns"`
	Webhooks       []string `yaml:"webhooks"`
	WebhookEvents  string   `yaml:"webhook_events"`
}

// TapConfig holds tap defaults.
type TapConfig struct {
	Namespace string `yaml:"namespace"`
	CPU       string `yaml:"cpu"`
	Memory    string `yaml:"memory"`
}

// DefaultsConfig holds global defaults.
type DefaultsConfig struct {
	Timeout string `yaml:"timeout"`
	Verbose bool   `yaml:"verbose"`
}

// Load reads config from ~/.logtap/config.yaml then CWD .logtap.yaml.
// CWD config values override home config. Missing files are not errors.
// Environment variables (LOGTAP_*) override config file values.
func Load() *Config {
	cfg := &Config{}

	// home config
	if home, err := os.UserHomeDir(); err == nil {
		_ = loadFile(filepath.Join(home, ".logtap", "config.yaml"), cfg)
	}

	// CWD config overrides
	_ = loadFile(".logtap.yaml", cfg)

	// env overrides
	applyEnv(cfg)

	return cfg
}

// LoadFrom reads config from a specific path. Used for testing.
func LoadFrom(path string) (*Config, error) {
	cfg := &Config{}
	if err := loadFile(path, cfg); err != nil {
		return nil, err
	}
	applyEnv(cfg)
	return cfg, nil
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, cfg)
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("LOGTAP_RECV_ADDR"); v != "" {
		cfg.Recv.Addr = v
	}
	if v := os.Getenv("LOGTAP_RECV_DIR"); v != "" {
		cfg.Recv.Dir = v
	}
	if v := os.Getenv("LOGTAP_RECV_DISK_CAP"); v != "" {
		cfg.Recv.DiskCap = v
	}
	if v := os.Getenv("LOGTAP_RECV_REDACT"); v != "" {
		cfg.Recv.Redact = v
	}
	if v := os.Getenv("LOGTAP_RECV_WEBHOOKS"); v != "" {
		cfg.Recv.Webhooks = strings.Split(v, ",")
	}
	if v := os.Getenv("LOGTAP_RECV_WEBHOOK_EVENTS"); v != "" {
		cfg.Recv.WebhookEvents = v
	}
	if v := os.Getenv("LOGTAP_TAP_NAMESPACE"); v != "" {
		cfg.Tap.Namespace = v
	}
	if v := os.Getenv("LOGTAP_TAP_CPU"); v != "" {
		cfg.Tap.CPU = v
	}
	if v := os.Getenv("LOGTAP_TAP_MEMORY"); v != "" {
		cfg.Tap.Memory = v
	}
	if v := os.Getenv("LOGTAP_TIMEOUT"); v != "" {
		cfg.Defaults.Timeout = v
	}
	if v := os.Getenv("LOGTAP_VERBOSE"); v != "" {
		cfg.Defaults.Verbose = strings.EqualFold(v, "true") || v == "1"
	}
}
