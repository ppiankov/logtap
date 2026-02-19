package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/config"
	"github.com/spf13/cobra"
)

func TestExecute_SubcommandRegistration(t *testing.T) {
	// Initialize global config so applyConfigDefaults doesn't panic
	cfg = config.Load()

	root := &cobra.Command{
		Use:   "logtap",
		Short: "Ephemeral log mirror for load testing",
	}
	root.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "timeout for cluster operations")
	root.AddCommand(newVersionCmd())
	root.AddCommand(newRecvCmd())
	root.AddCommand(newOpenCmd())
	root.AddCommand(newInspectCmd())
	root.AddCommand(newSliceCmd())
	root.AddCommand(newExportCmd())
	root.AddCommand(newTriageCmd())
	root.AddCommand(newGrepCmd())
	root.AddCommand(newMergeCmd())
	root.AddCommand(newSnapshotCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newCompletionCmd())
	root.AddCommand(newTapCmd())
	root.AddCommand(newUntapCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newDeployCmd())
	root.AddCommand(newUploadCmd())
	root.AddCommand(newDownloadCmd())
	root.AddCommand(newGCCmd())
	root.AddCommand(newWatchCmd())

	expected := []string{
		"version", "recv", "open", "inspect", "slice", "export", "triage",
		"grep", "merge", "snapshot", "diff", "completion",
		"tap", "untap", "check", "status", "deploy", "upload", "download", "gc",
		"watch",
	}

	commands := make(map[string]bool)
	for _, c := range root.Commands() {
		commands[c.Name()] = true
	}

	for _, name := range expected {
		if !commands[name] {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestSubcommandHelp(t *testing.T) {
	cfg = config.Load()

	cmds := []func() *cobra.Command{
		newVersionCmd,
		newRecvCmd,
		newOpenCmd,
		newInspectCmd,
		newSliceCmd,
		newExportCmd,
		newTriageCmd,
		newGrepCmd,
		newMergeCmd,
		newSnapshotCmd,
		newDiffCmd,
		newCompletionCmd,
		newTapCmd,
		newUntapCmd,
		newCheckCmd,
		newStatusCmd,
		newDeployCmd,
		newUploadCmd,
		newDownloadCmd,
		newGCCmd,
		newWatchCmd,
	}

	for _, newCmd := range cmds {
		cmd := newCmd()
		t.Run(cmd.Name(), func(t *testing.T) {
			if cmd.Use == "" {
				t.Error("Use is empty")
			}
			if cmd.Short == "" {
				t.Error("Short is empty")
			}

			root := &cobra.Command{Use: "logtap"}
			root.AddCommand(cmd)

			var buf bytes.Buffer
			root.SetOut(&buf)
			root.SetErr(&buf)
			root.SetArgs([]string{cmd.Name(), "--help"})
			if err := root.Execute(); err != nil {
				t.Errorf("%s --help: %v", cmd.Name(), err)
			}
		})
	}
}

func TestClusterContext_DefaultTimeout(t *testing.T) {
	// Reset globals
	oldTimeout := timeoutStr
	oldCfg := cfg
	defer func() {
		timeoutStr = oldTimeout
		cfg = oldCfg
	}()

	timeoutStr = ""
	cfg = nil

	ctx, cancel := clusterContext()
	defer cancel()

	_, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have deadline")
	}
}

func TestClusterContext_FlagOverride(t *testing.T) {
	oldTimeout := timeoutStr
	oldCfg := cfg
	defer func() {
		timeoutStr = oldTimeout
		cfg = oldCfg
	}()

	timeoutStr = "5s"
	cfg = nil

	ctx, cancel := clusterContext()
	defer cancel()

	_, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have deadline")
	}
}

func TestClusterContext_ConfigOverride(t *testing.T) {
	oldTimeout := timeoutStr
	oldCfg := cfg
	defer func() {
		timeoutStr = oldTimeout
		cfg = oldCfg
	}()

	timeoutStr = ""
	cfg = &config.Config{
		Defaults: config.DefaultsConfig{Timeout: "10s"},
	}

	ctx, cancel := clusterContext()
	defer cancel()

	_, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have deadline")
	}
}

func TestApplyConfigDefaults_NilConfig(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()
	cfg = nil

	cmd := &cobra.Command{}
	cmd.Flags().String("listen", "", "")
	// Should not panic
	applyConfigDefaults(cmd)
}

func TestApplyConfigDefaults_SetsValues(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()
	cfg = &config.Config{
		Recv: config.RecvConfig{
			Addr: ":5000",
			Dir:  "/tmp/test",
		},
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("listen", "", "")
	cmd.Flags().String("dir", "", "")

	applyConfigDefaults(cmd)

	if v, _ := cmd.Flags().GetString("listen"); v != ":5000" {
		t.Errorf("expected listen :5000, got %q", v)
	}
	if v, _ := cmd.Flags().GetString("dir"); v != "/tmp/test" {
		t.Errorf("expected dir /tmp/test, got %q", v)
	}
}

func TestApplyConfigDefaults_FlagPrecedence(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()
	cfg = &config.Config{
		Recv: config.RecvConfig{
			Addr: ":5000",
		},
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("listen", "", "")

	// Simulate flag being explicitly set
	_ = cmd.Flags().Set("listen", ":9999")

	applyConfigDefaults(cmd)

	// Flag should win over config
	if v, _ := cmd.Flags().GetString("listen"); v != ":9999" {
		t.Errorf("expected flag value :9999, got %q", v)
	}
}

func TestApplyConfigDefaults_TapFields(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()
	cfg = &config.Config{
		Tap: config.TapConfig{
			Namespace: "staging",
			CPU:       "50m",
			Memory:    "32Mi",
		},
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("namespace", "", "")
	cmd.Flags().String("cpu", "", "")
	cmd.Flags().String("memory", "", "")

	applyConfigDefaults(cmd)

	if v, _ := cmd.Flags().GetString("namespace"); v != "staging" {
		t.Errorf("expected namespace staging, got %q", v)
	}
	if v, _ := cmd.Flags().GetString("cpu"); v != "50m" {
		t.Errorf("expected cpu 50m, got %q", v)
	}
	if v, _ := cmd.Flags().GetString("memory"); v != "32Mi" {
		t.Errorf("expected memory 32Mi, got %q", v)
	}
}

func TestApplyConfigDefaults_RecvAllFields(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()
	cfg = &config.Config{
		Recv: config.RecvConfig{
			Addr:           ":5000",
			Dir:            "/tmp/captures",
			DiskCap:        "10GB",
			Redact:         "true",
			RedactPatterns: "/path/to/patterns.yaml",
		},
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("listen", "", "")
	cmd.Flags().String("dir", "", "")
	cmd.Flags().String("max-disk", "", "")
	cmd.Flags().String("redact", "", "")
	cmd.Flags().String("redact-patterns", "", "")

	applyConfigDefaults(cmd)

	if v, _ := cmd.Flags().GetString("max-disk"); v != "10GB" {
		t.Errorf("expected max-disk 10GB, got %q", v)
	}
	if v, _ := cmd.Flags().GetString("redact"); v != "true" {
		t.Errorf("expected redact true, got %q", v)
	}
	if v, _ := cmd.Flags().GetString("redact-patterns"); v != "/path/to/patterns.yaml" {
		t.Errorf("expected redact-patterns path, got %q", v)
	}
}

func TestRunInspect_InvalidDir(t *testing.T) {
	err := runInspect("/nonexistent/dir", false)
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestRunInspect_InvalidDirJSON(t *testing.T) {
	err := runInspect("/nonexistent/dir", true)
	if err == nil {
		t.Error("expected error for nonexistent dir with json flag")
	}
}

func TestRunDiff_InvalidDirs(t *testing.T) {
	err := runDiff("/nonexistent/a", "/nonexistent/b", false)
	if err == nil {
		t.Error("expected error for nonexistent dirs")
	}
}

func TestRunSlice_InvalidDir(t *testing.T) {
	err := runSlice("/nonexistent/dir", "", "", nil, "", "/tmp/out")
	if err == nil {
		t.Error("expected error for nonexistent source dir")
	}
}

func TestRunExport_InvalidFormat(t *testing.T) {
	err := runExport("/nonexistent/dir", "xml", "", "", nil, "", "/tmp/out")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestRunExport_InvalidDir(t *testing.T) {
	err := runExport("/nonexistent/dir", "csv", "", "", nil, "", "/tmp/out")
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestRunGrep_InvalidDir(t *testing.T) {
	err := runGrep("pattern", "/nonexistent/dir", "", "", nil, false, false, "json")
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestRunMerge_InvalidDirs(t *testing.T) {
	err := runMerge([]string{"/nonexistent/a", "/nonexistent/b"}, "/tmp/out")
	if err == nil {
		t.Error("expected error for nonexistent source dirs")
	}
}

func TestRunSnapshot_PackInvalidDir(t *testing.T) {
	err := runSnapshot("/nonexistent/dir", "/tmp/out.tar.zst", false)
	if err == nil {
		t.Error("expected error for nonexistent source dir")
	}
}

func TestRunSnapshot_ExtractInvalidFile(t *testing.T) {
	err := runSnapshot("/nonexistent/file.tar.zst", "/tmp/out", true)
	if err == nil {
		t.Error("expected error for nonexistent archive file")
	}
}

func TestRunOpen_InvalidDir(t *testing.T) {
	err := runOpen("/nonexistent/dir", "1", "", "", nil, "")
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestRunTriage_InvalidDir(t *testing.T) {
	err := runTriage("/nonexistent/dir", "/tmp/out", 1, 60000000000, 50, false, false)
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestRunRecv_InvalidByteSize(t *testing.T) {
	err := runRecv(":3100", "/tmp", "invalid", "50GB", true, "", "", 100, true, "", "", nil, "", "")
	if err == nil {
		t.Error("expected error for invalid max-file size")
	}
}

func TestRunRecv_InvalidDiskSize(t *testing.T) {
	err := runRecv(":3100", "/tmp", "256MB", "invalid", true, "", "", 100, true, "", "", nil, "", "")
	if err == nil {
		t.Error("expected error for invalid max-disk size")
	}
}

func TestRunRecv_InvalidRedactPatterns(t *testing.T) {
	dir := t.TempDir()
	err := runRecv(":0", dir, "256MB", "50GB", true, "true", "/nonexistent/patterns.yaml", 100, true, "", "", nil, "", "")
	if err == nil {
		t.Error("expected error for nonexistent redact patterns file")
	}
}

func TestRunRecv_MissingDir(t *testing.T) {
	// --dir is required
	err := runRecv(":0", "", "256MB", "50GB", true, "", "", 100, true, "", "", nil, "", "")
	// We check this in the command RunE, but runRecv itself creates the dir.
	// Pass an empty dir — os.MkdirAll("") may fail on some systems.
	// Just verify it doesn't panic.
	_ = err
}

func TestRunRecv_InvalidRedactName(t *testing.T) {
	dir := t.TempDir()
	err := runRecv(":0", dir, "256MB", "50GB", true, "nonexistent_pattern_name", "", 100, true, "", "", nil, "", "")
	if err == nil {
		t.Error("expected error for invalid redact pattern name")
	}
}

func TestExecute_Version(t *testing.T) {
	oldVersion := version
	oldCommit := commit
	oldDate := date
	defer func() { version = oldVersion; commit = oldCommit; date = oldDate }()
	version = "1.2.3"
	commit = "abc1234"
	date = "2026-01-01T00:00:00Z"

	cfg = config.Load()
	root := &cobra.Command{
		Use:   "logtap",
		Short: "Ephemeral log mirror for load testing",
	}
	root.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "timeout")
	root.AddCommand(newVersionCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Errorf("execute version: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "logtap 1.2.3") {
		t.Errorf("expected version in output, got: %s", out)
	}
	if !strings.Contains(out, "commit: abc1234") {
		t.Errorf("expected commit in output, got: %s", out)
	}
}

func TestExecute_VersionJSON(t *testing.T) {
	oldVersion := version
	defer func() { version = oldVersion }()
	version = "1.2.3"

	cfg = config.Load()
	root := &cobra.Command{
		Use:   "logtap",
		Short: "Ephemeral log mirror for load testing",
	}
	root.AddCommand(newVersionCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"version", "--json"})
	if err := root.Execute(); err != nil {
		t.Errorf("execute version --json: %v", err)
	}

	var info buildInfo
	if err := json.Unmarshal(buf.Bytes(), &info); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if info.Version != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", info.Version)
	}
}

func TestExecute_Help(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{
		Use:   "logtap",
		Short: "Ephemeral log mirror for load testing",
	}
	root.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "timeout")
	root.AddCommand(newRecvCmd())
	root.AddCommand(newTapCmd())
	root.AddCommand(newUntapCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newStatusCmd())

	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("execute --help: %v", err)
	}
}

func TestSubcommand_TapHelp(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newTapCmd())
	root.SetArgs([]string{"tap", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("tap --help: %v", err)
	}
}

func TestSubcommand_RecvHelp(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newRecvCmd())
	root.SetArgs([]string{"recv", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("recv --help: %v", err)
	}
}

func TestCompletionGeneration(t *testing.T) {
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newCompletionCmd())

	// Test that completion subcommand exists
	for _, child := range root.Commands() {
		if child.Name() == "completion" {
			if len(child.ValidArgs) != 3 {
				t.Errorf("expected 3 valid args (bash, zsh, fish), got %d", len(child.ValidArgs))
			}
			return
		}
	}
	t.Error("completion command not found")
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestCobraGC_MissingArgs(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newGCCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"gc"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for missing gc args")
	}
}

func TestCobraGC_MissingFlags(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newGCCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"gc", "/tmp"})
	if err := root.Execute(); err == nil {
		t.Error("expected error when no --max-age or --max-total")
	}
}

func TestCobraGC_WithArgs(t *testing.T) {
	dir := t.TempDir()

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newGCCmd())

	restore := redirectOutput(t)
	defer restore()

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"gc", dir, "--max-age", "24h", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Errorf("gc --max-age: %v", err)
	}
}

func TestCobraUpload_MissingTo(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newUploadCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"upload", "/tmp"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for missing --to flag")
	}
}

func TestCobraUpload_InvalidCapture(t *testing.T) {
	dir := t.TempDir()

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newUploadCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"upload", dir, "--to", "s3://bucket/prefix"})
	// Will fail because dir is not a valid capture (no metadata.json)
	if err := root.Execute(); err == nil {
		t.Error("expected error for non-capture dir")
	}
}

func TestCobraDownload_MissingOut(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newDownloadCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"download", "s3://bucket/prefix"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for missing --out flag")
	}
}

func TestCobraInspect_WithJSON(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newInspectCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"inspect", dir, "--json"})
	if err := root.Execute(); err != nil {
		t.Errorf("inspect --json: %v", err)
	}
}

func TestCobraDiff_Success(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	dirA := makeCaptureDir(t, sampleEntries(base))
	dirB := makeCaptureDir(t, sampleEntries(base.Add(5*time.Second)))

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newDiffCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"diff", dirA, dirB})
	if err := root.Execute(); err != nil {
		t.Errorf("diff: %v", err)
	}
}

func TestCobraSnapshot_Pack(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	archivePath := filepath.Join(t.TempDir(), "test.tar.zst")

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newSnapshotCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"snapshot", dir, "--output", archivePath})
	if err := root.Execute(); err != nil {
		t.Errorf("snapshot pack: %v", err)
	}
}

func TestCobraGrep_Success(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newGrepCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"grep", "error", dir})
	if err := root.Execute(); err != nil {
		t.Errorf("grep: %v", err)
	}
}

func TestCobraSlice_Success(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outDir := filepath.Join(t.TempDir(), "sliced")

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newSliceCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"slice", dir, "--out", outDir})
	if err := root.Execute(); err != nil {
		t.Errorf("slice: %v", err)
	}
}

func TestCobraExport_Success(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outPath := filepath.Join(t.TempDir(), "export.jsonl")

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newExportCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"export", dir, "--format", "jsonl", "--out", outPath})
	if err := root.Execute(); err != nil {
		t.Errorf("export: %v", err)
	}
}

func TestCobraMerge_Success(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	dirA := makeCaptureDir(t, sampleEntries(base))
	dirB := makeCaptureDir(t, sampleEntries(base.Add(10*time.Second)))
	outDir := filepath.Join(t.TempDir(), "merged")

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newMergeCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"merge", dirA, dirB, "--out", outDir})
	if err := root.Execute(); err != nil {
		t.Errorf("merge: %v", err)
	}
}

func TestCobraTriage_JSON(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newTriageCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"triage", dir, "--json"})
	if err := root.Execute(); err != nil {
		t.Errorf("triage --json: %v", err)
	}
}

func TestCobraTriage_HTML(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outDir := filepath.Join(t.TempDir(), "triage")

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newTriageCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"triage", dir, "--out", outDir, "--html"})
	if err := root.Execute(); err != nil {
		t.Errorf("triage --html: %v", err)
	}
}

func TestParseByteSize_AllUnits(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"100", 100},
		{"100B", 100},
		{"1KB", 1024},
		{"1MB", 1 << 20},
		{"256MB", 256 << 20},
		{"1GB", 1 << 30},
		{"50GB", 50 << 30},
		{"1TB", 1 << 40},
		{"0.5GB", 1 << 29},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseByteSize(tt.input)
			if err != nil {
				t.Fatalf("parseByteSize(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseByteSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestExecute_GCHelp(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newGCCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"gc", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("gc --help: %v", err)
	}
}

func TestCobraOpen_InvalidDir(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newOpenCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"open", "/nonexistent/dir"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestCobraOpen_InvalidSpeed(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newOpenCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"open", dir, "--speed", "notaspeed"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for invalid speed")
	}
}

func TestParseSpeed_Values(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"0", 0},
		{"1", 1},
		{"10", 10},
		{"10x", 10},
		{"0.5", 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSpeed(tt.input)
			if err != nil {
				t.Fatalf("parseSpeed(%q): %v", tt.input, err)
			}
			if float64(got) != tt.want {
				t.Errorf("parseSpeed(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSpeed_Invalid(t *testing.T) {
	invalids := []string{"abc", "notaspeed", "-1"}
	for _, input := range invalids {
		t.Run(input, func(t *testing.T) {
			_, err := parseSpeed(input)
			if err == nil {
				t.Errorf("expected error for %q", input)
			}
		})
	}
}

func TestParseByteSize_Invalid(t *testing.T) {
	invalids := []string{"", "abc", "MB", "-5MB"}
	for _, input := range invalids {
		t.Run(input, func(t *testing.T) {
			_, err := parseByteSize(input)
			if err == nil {
				t.Errorf("expected error for %q", input)
			}
		})
	}
}

func TestClusterContext_InvalidFlagFallsBack(t *testing.T) {
	oldTimeout := timeoutStr
	oldCfg := cfg
	defer func() {
		timeoutStr = oldTimeout
		cfg = oldCfg
	}()

	timeoutStr = "not-a-duration"
	cfg = nil

	ctx, cancel := clusterContext()
	defer cancel()

	// Should fall back to default timeout, still have a deadline
	_, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have deadline even with invalid timeout string")
	}
}
