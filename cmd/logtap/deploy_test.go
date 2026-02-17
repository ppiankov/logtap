package main

import (
	"bytes"
	"testing"

	"github.com/ppiankov/logtap/internal/config"
	"github.com/spf13/cobra"
)

func TestDeployCmd_Help(t *testing.T) {
	cfg = config.Load()

	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newDeployCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"deploy", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("deploy --help: %v", err)
	}
}

func TestDeployCmd_FlagDefaults(t *testing.T) {
	cfg = config.Load()

	cmd := newDeployCmd()

	if v, _ := cmd.Flags().GetString("image"); v != defaultRecvImage {
		t.Errorf("image default = %q, want %q", v, defaultRecvImage)
	}
	if v, _ := cmd.Flags().GetInt32("port"); v != defaultRecvPort {
		t.Errorf("port default = %d, want %d", v, defaultRecvPort)
	}
	if v, _ := cmd.Flags().GetString("max-disk"); v != defaultMaxDisk {
		t.Errorf("max-disk default = %q, want %q", v, defaultMaxDisk)
	}
	if v, _ := cmd.Flags().GetBool("cleanup"); v {
		t.Error("cleanup default should be false")
	}
	if v, _ := cmd.Flags().GetBool("dry-run"); v {
		t.Error("dry-run default should be false")
	}
}

func TestDeployCmd_DryRun(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()
	cfg = config.Load()

	// dry-run calls k8s.NewClient which fails without kubeconfig
	// but the error path is still exercised
	root := &cobra.Command{Use: "logtap"}
	root.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "timeout")
	root.AddCommand(newDeployCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"deploy", "--dry-run", "-n", "test-ns"})
	// Will fail with "connect to cluster" error (no kubeconfig in CI)
	// We just verify it doesn't panic and the error is sensible
	err := root.Execute()
	if err == nil {
		// If it succeeds (e.g. kubeconfig exists), that's fine too
		return
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestDeployCmd_CleanupDryRun(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()
	cfg = config.Load()

	root := &cobra.Command{Use: "logtap"}
	root.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "timeout")
	root.AddCommand(newDeployCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"deploy", "--cleanup", "--dry-run", "-n", "test-ns"})
	err := root.Execute()
	if err == nil {
		return
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestDeployCmd_Registration(t *testing.T) {
	cfg = config.Load()

	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newDeployCmd())

	found := false
	for _, c := range root.Commands() {
		if c.Name() == "deploy" {
			found = true
			if c.Short == "" {
				t.Error("deploy Short is empty")
			}
			break
		}
	}
	if !found {
		t.Error("deploy command not registered")
	}
}
