package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const defaultConfig = `# logtap configuration
# See: https://github.com/ppiankov/logtap

# recv:
#   addr: ":9000"
#   dir: "./capture"
#   disk_cap: "50GB"
#   redact: "false"

# tap:
#   namespace: ""
#   cpu: "25m"
#   memory: "16Mi"

# defaults:
#   timeout: "30s"
#   verbose: false
`

type initResult struct {
	ConfigPath string `json:"config_path"`
	Created    bool   `json:"created"`
	LocalPath  string `json:"local_path"`
	LocalFound bool   `json:"local_found"`
}

func newInitCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize logtap configuration",
		Long:  "Create a default config file at ~/.logtap/config.yaml if it does not exist, and show config file locations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	addFormatAlias(cmd, &jsonOutput)
	return cmd
}

func runInit(jsonOutput bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	configDir := filepath.Join(home, ".logtap")
	configPath := filepath.Join(configDir, "config.yaml")

	result := initResult{
		ConfigPath: configPath,
		LocalPath:  ".logtap.yaml",
	}

	// Check local config
	if _, err := os.Stat(".logtap.yaml"); err == nil {
		result.LocalFound = true
	}

	// Create config if missing
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}
		if err := os.WriteFile(configPath, []byte(defaultConfig), 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		result.Created = true
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if result.Created {
		fmt.Fprintf(os.Stderr, "Created:  %s\n", configPath)
	} else {
		fmt.Fprintf(os.Stderr, "Exists:   %s\n", configPath)
	}
	if result.LocalFound {
		fmt.Fprintf(os.Stderr, "Local:    .logtap.yaml (overrides home config)\n")
	}
	fmt.Fprintf(os.Stderr, "\nEdit %s to set defaults for recv, tap, and timeout.\n", configPath)
	return nil
}
