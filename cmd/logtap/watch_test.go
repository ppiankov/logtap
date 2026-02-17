package main

import (
	"bytes"
	"testing"

	"github.com/ppiankov/logtap/internal/config"
	"github.com/spf13/cobra"
)

func TestWatchCmd_Help(t *testing.T) {
	cfg = config.Load()

	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newWatchCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"watch", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("watch --help: %v", err)
	}
}

func TestWatchCmd_FlagDefaults(t *testing.T) {
	cmd := newWatchCmd()

	if v, _ := cmd.Flags().GetInt("lines"); v != 10 {
		t.Errorf("lines default = %d, want 10", v)
	}
	if v, _ := cmd.Flags().GetString("grep"); v != "" {
		t.Errorf("grep default = %q, want empty", v)
	}
	if v, _ := cmd.Flags().GetString("label"); v != "" {
		t.Errorf("label default = %q, want empty", v)
	}
}

func TestWatchCmd_InvalidDir(t *testing.T) {
	cfg = config.Load()

	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newWatchCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"watch", "/nonexistent/dir"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestWatchCmd_InvalidGrep(t *testing.T) {
	dir := t.TempDir()

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newWatchCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"watch", dir, "--grep", "["})
	if err := root.Execute(); err == nil {
		t.Error("expected error for invalid grep regex")
	}
}

func TestWatchCmd_InvalidLabel(t *testing.T) {
	dir := t.TempDir()

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newWatchCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"watch", dir, "--label", "noequalssign"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for invalid label filter")
	}
}
