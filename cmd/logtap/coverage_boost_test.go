package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/cli"
	"github.com/ppiankov/logtap/internal/config"
	"github.com/ppiankov/logtap/internal/recv"
	"github.com/spf13/cobra"
)

func TestHasJSONFlag(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"logtap", "version"}, false},
		{[]string{"logtap", "version", "--json"}, true},
		{[]string{"logtap", "--json", "version"}, true},
		{nil, false},
		{[]string{"--json"}, true},
	}
	for _, tt := range tests {
		got := hasJSONFlag(tt.args)
		if got != tt.want {
			t.Errorf("hasJSONFlag(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestClassifyError_Nil(t *testing.T) {
	if classifyError(nil) != nil {
		t.Error("expected nil for nil error")
	}
}

func TestClassifyError_AlreadyCLIError(t *testing.T) {
	orig := cli.NewNotFoundError("already classified")
	result := classifyError(orig)
	var ce *cli.CLIError
	if !errors.As(result, &ce) {
		t.Error("expected CLIError to pass through")
	}
}

func TestClassifyError_NotFound(t *testing.T) {
	tests := []error{
		os.ErrNotExist,
		errors.New("no such file or directory"),
		errors.New("metadata.json not found"),
		errors.New("not a valid capture directory"),
	}
	for _, err := range tests {
		result := classifyError(err)
		var ce *cli.CLIError
		if !errors.As(result, &ce) {
			t.Errorf("expected CLIError for %q", err)
		}
	}
}

func TestClassifyError_Permission(t *testing.T) {
	tests := []error{
		os.ErrPermission,
		errors.New("permission denied"),
	}
	for _, err := range tests {
		result := classifyError(err)
		var ce *cli.CLIError
		if !errors.As(result, &ce) {
			t.Errorf("expected CLIError for %q", err)
		}
	}
}

func TestClassifyError_Network(t *testing.T) {
	tests := []error{
		errors.New("connection refused"),
		errors.New("timeout waiting for response"),
		errors.New("no such host"),
		errors.New("network is unreachable"),
	}
	for _, err := range tests {
		result := classifyError(err)
		var ce *cli.CLIError
		if !errors.As(result, &ce) {
			t.Errorf("expected CLIError for %q", err)
		}
	}
}

func TestClassifyError_Generic(t *testing.T) {
	err := errors.New("some generic error")
	result := classifyError(err)
	var ce *cli.CLIError
	if errors.As(result, &ce) {
		t.Error("generic error should not be wrapped in CLIError")
	}
	if result.Error() != err.Error() {
		t.Errorf("expected original error, got %q", result.Error())
	}
}

func TestRunCatalog_InvalidDir(t *testing.T) {
	err := runCatalog("/nonexistent/dir", false, false)
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestRunCatalog_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	restore := redirectOutput(t)
	defer restore()

	if err := runCatalog(dir, false, false); err != nil {
		t.Fatalf("runCatalog empty dir: %v", err)
	}
}

func TestRunCatalog_EmptyDirJSON(t *testing.T) {
	dir := t.TempDir()
	restore := redirectOutput(t)
	defer restore()

	if err := runCatalog(dir, false, true); err != nil {
		t.Fatalf("runCatalog empty dir json: %v", err)
	}
}

func TestRunCatalog_WithCapture(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	capDir := filepath.Join(root, "cap-1")
	if err := os.MkdirAll(capDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := &recv.Metadata{
		Version:    1,
		Format:     "jsonl",
		Started:    base,
		Stopped:    base.Add(time.Hour),
		TotalLines: 100,
	}
	if err := recv.WriteMetadata(capDir, meta); err != nil {
		t.Fatal(err)
	}

	restore := redirectOutput(t)
	defer restore()

	if err := runCatalog(root, false, false); err != nil {
		t.Fatalf("runCatalog: %v", err)
	}
}

func TestRunCatalog_Recursive(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	subDir := filepath.Join(root, "sub", "cap-nested")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := &recv.Metadata{
		Version: 1,
		Format:  "jsonl",
		Started: base,
	}
	if err := recv.WriteMetadata(subDir, meta); err != nil {
		t.Fatal(err)
	}

	restore := redirectOutput(t)
	defer restore()

	if err := runCatalog(root, true, false); err != nil {
		t.Fatalf("runCatalog recursive: %v", err)
	}
}

func TestCobraCatalog_Help(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newCatalogCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"catalog", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("catalog --help: %v", err)
	}
}

func TestCobraCatalog_FlagDefaults(t *testing.T) {
	cmd := newCatalogCmd()

	if v, _ := cmd.Flags().GetBool("json"); v {
		t.Error("json default should be false")
	}
	if v, _ := cmd.Flags().GetBool("recursive"); v {
		t.Error("recursive default should be false")
	}
}

func TestCobraReport_Help(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newReportCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"report", "--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("report --help: %v", err)
	}
}

func TestCobraReport_FlagDefaults(t *testing.T) {
	cmd := newReportCmd()

	if v, _ := cmd.Flags().GetBool("json"); v {
		t.Error("json default should be false")
	}
	if v, _ := cmd.Flags().GetBool("html"); !v {
		t.Error("html default should be true")
	}
	if v, _ := cmd.Flags().GetInt("top"); v != 20 {
		t.Errorf("top default = %d, want 20", v)
	}
}

func TestRunReport_InvalidDir(t *testing.T) {
	err := runReport("/nonexistent/dir", "", false, false, 1, 5)
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestRunReport_Success_JSON(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	restore := redirectOutput(t)
	defer restore()

	if err := runReport(dir, "", true, false, 1, 5); err != nil {
		t.Fatalf("runReport json: %v", err)
	}
}

func TestRunReport_MissingOut(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	restore := redirectOutput(t)
	defer restore()

	err := runReport(dir, "", false, false, 1, 5)
	if err == nil {
		t.Fatal("expected error when --out not set and --json not used")
	}
	if !strings.Contains(err.Error(), "--out is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunReport_WithOut(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outDir := filepath.Join(t.TempDir(), "report-out")

	restore := redirectOutput(t)
	defer restore()

	if err := runReport(dir, outDir, false, true, 1, 5); err != nil {
		t.Fatalf("runReport with out: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "report.json")); err != nil {
		t.Errorf("report.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "report.html")); err != nil {
		t.Errorf("report.html missing: %v", err)
	}
}

func TestRunBaselineDiff_InvalidDirs(t *testing.T) {
	err := runBaselineDiff("/nonexistent/a", "/nonexistent/b", false)
	if err == nil {
		t.Error("expected error for nonexistent dirs")
	}
}

func TestRunBaselineDiff_Success(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	dirA := makeCaptureDir(t, sampleEntries(base))
	dirB := makeCaptureDir(t, sampleEntries(base.Add(5*time.Second)))

	restore := redirectOutput(t)
	defer restore()

	if err := runBaselineDiff(dirA, dirB, false); err != nil {
		t.Fatalf("runBaselineDiff text: %v", err)
	}
}

func TestRunBaselineDiff_JSON(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	dirA := makeCaptureDir(t, sampleEntries(base))
	dirB := makeCaptureDir(t, sampleEntries(base.Add(5*time.Second)))

	restore := redirectOutput(t)
	defer restore()

	if err := runBaselineDiff(dirA, dirB, true); err != nil {
		t.Fatalf("runBaselineDiff json: %v", err)
	}
}

func TestCobraDiff_Baseline(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	dirA := makeCaptureDir(t, sampleEntries(base))
	dirB := makeCaptureDir(t, sampleEntries(base.Add(5*time.Second)))

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newDiffCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"diff", dirA, dirB, "--baseline"})
	if err := root.Execute(); err != nil {
		t.Errorf("diff --baseline: %v", err)
	}
}

func TestParseTime_RFC3339(t *testing.T) {
	ts, err := parseTime("2025-01-15T10:30:00Z")
	if err != nil {
		t.Fatalf("parseTime RFC3339: %v", err)
	}
	if ts.Hour() != 10 || ts.Minute() != 30 {
		t.Errorf("unexpected time: %v", ts)
	}
}

func TestParseTime_RFC3339Nano(t *testing.T) {
	ts, err := parseTime("2025-01-15T10:30:00.123456789Z")
	if err != nil {
		t.Fatalf("parseTime RFC3339Nano: %v", err)
	}
	if ts.Hour() != 10 || ts.Minute() != 30 {
		t.Errorf("unexpected time: %v", ts)
	}
}

func TestParseTime_HHMM(t *testing.T) {
	ts, err := parseTime("10:30")
	if err != nil {
		t.Fatalf("parseTime HH:MM: %v", err)
	}
	if ts.Hour() != 10 || ts.Minute() != 30 {
		t.Errorf("unexpected time: %v", ts)
	}
}

func TestParseTime_Relative(t *testing.T) {
	before := time.Now()
	ts, err := parseTime("-30m")
	if err != nil {
		t.Fatalf("parseTime relative: %v", err)
	}
	expected := before.Add(-30 * time.Minute)
	diff := ts.Sub(expected)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("relative time off by %v", diff)
	}
}

func TestParseTime_DurationSuffix(t *testing.T) {
	before := time.Now()
	ts, err := parseTime("1h")
	if err != nil {
		t.Fatalf("parseTime 1h: %v", err)
	}
	expected := before.Add(-1 * time.Hour)
	diff := ts.Sub(expected)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("duration time off by %v", diff)
	}
}

func TestParseTime_Invalid(t *testing.T) {
	_, err := parseTime("not-a-time")
	if err == nil {
		t.Error("expected error for invalid time")
	}
}

func TestRunSnapshot_JSONPack(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	archivePath := filepath.Join(t.TempDir(), "test.tar.zst")

	restore := redirectOutput(t)
	defer restore()

	if err := runSnapshot(dir, archivePath, false, true); err != nil {
		t.Fatalf("runSnapshot json pack: %v", err)
	}
}

func TestRunSnapshot_JSONExtract(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	archivePath := filepath.Join(t.TempDir(), "test.tar.zst")
	extractDir := filepath.Join(t.TempDir(), "extract")

	restore := redirectOutput(t)
	defer restore()

	if err := runSnapshot(dir, archivePath, false, false); err != nil {
		t.Fatalf("runSnapshot pack: %v", err)
	}
	if err := runSnapshot(archivePath, extractDir, true, true); err != nil {
		t.Fatalf("runSnapshot json extract: %v", err)
	}
}

func TestRunExport_JSONOutput(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outPath := filepath.Join(t.TempDir(), "export.jsonl")

	restore := redirectOutput(t)
	defer restore()

	if err := runExport(dir, "jsonl", "", "", nil, "", outPath, true); err != nil {
		t.Fatalf("runExport json output: %v", err)
	}
}

func TestRunMerge_JSONOutput(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	dirA := makeCaptureDir(t, sampleEntries(base))
	dirB := makeCaptureDir(t, sampleEntries(base.Add(10*time.Second)))
	outDir := filepath.Join(t.TempDir(), "merged")

	restore := redirectOutput(t)
	defer restore()

	if err := runMerge([]string{dirA, dirB}, outDir, true); err != nil {
		t.Fatalf("runMerge json: %v", err)
	}
}

func TestRunTriage_MissingOut(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	restore := redirectOutput(t)
	defer restore()

	err := runTriage(dir, "", 1, time.Minute, 5, 10000, false, false)
	if err == nil {
		t.Fatal("expected error when --out not set and --json not used")
	}
	if !strings.Contains(err.Error(), "--out is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCobraSlice_InvalidLabel(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outDir := filepath.Join(t.TempDir(), "sliced")

	err := runSlice(dir, "", "", []string{"badlabel"}, "", outDir)
	if err == nil {
		t.Error("expected error for invalid label")
	}
}

func TestCobraSlice_InvalidGrep(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outDir := filepath.Join(t.TempDir(), "sliced")

	err := runSlice(dir, "", "", nil, "[invalid(", outDir)
	if err == nil {
		t.Error("expected error for invalid grep regex")
	}
}

func TestApplyConfigDefaults_WebhookEvents(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()
	cfg = &config.Config{
		Recv: config.RecvConfig{
			WebhookEvents: "start,stop",
		},
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("webhook-events", "", "")

	applyConfigDefaults(cmd)

	if v, _ := cmd.Flags().GetString("webhook-events"); v != "start,stop" {
		t.Errorf("expected webhook-events 'start,stop', got %q", v)
	}
}

func TestEntryLabel_AppLabel(t *testing.T) {
	e := recv.LogEntry{
		Labels:  map[string]string{"app": "web", "env": "staging"},
		Message: "test",
	}
	got := entryLabel(e)
	if got != "web" {
		t.Errorf("entryLabel = %q, want 'web'", got)
	}
}

func TestEntryLabel_FallbackToFirst(t *testing.T) {
	e := recv.LogEntry{
		Labels:  map[string]string{"env": "staging"},
		Message: "test",
	}
	got := entryLabel(e)
	if got != "staging" {
		t.Errorf("entryLabel = %q, want 'staging'", got)
	}
}

func TestEntryLabel_NoLabels(t *testing.T) {
	e := recv.LogEntry{
		Labels:  map[string]string{},
		Message: "test",
	}
	got := entryLabel(e)
	if got != "-" {
		t.Errorf("entryLabel = %q, want '-'", got)
	}
}

func TestRunGrep_Context(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	restore := redirectOutput(t)
	defer restore()

	if err := runGrep("error", dir, "", "", nil, false, false, "json", 1); err != nil {
		t.Fatalf("runGrep context: %v", err)
	}
}

func TestRunGrep_TextWithContext(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	restore := redirectOutput(t)
	defer restore()

	if err := runGrep("error", dir, "", "", nil, false, false, "text", 1); err != nil {
		t.Fatalf("runGrep text with context: %v", err)
	}
}

func TestSubcommandRegistration_CatalogAndReport(t *testing.T) {
	cfg = config.Load()

	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newCatalogCmd())
	root.AddCommand(newReportCmd())

	commands := make(map[string]bool)
	for _, c := range root.Commands() {
		commands[c.Name()] = true
	}

	if !commands["catalog"] {
		t.Error("missing subcommand: catalog")
	}
	if !commands["report"] {
		t.Error("missing subcommand: report")
	}
}

func TestCobraSnapshot_JSONFlags(t *testing.T) {
	cmd := newSnapshotCmd()
	if v, _ := cmd.Flags().GetBool("json"); v {
		t.Error("json default should be false")
	}
	if v, _ := cmd.Flags().GetBool("extract"); v {
		t.Error("extract default should be false")
	}
}

func TestCobraExport_Flags(t *testing.T) {
	cmd := newExportCmd()
	if v, _ := cmd.Flags().GetBool("json"); v {
		t.Error("json default should be false")
	}
}

func TestCobraMerge_Flags(t *testing.T) {
	cmd := newMergeCmd()
	if v, _ := cmd.Flags().GetBool("json"); v {
		t.Error("json default should be false")
	}
}

func TestCobraDiff_Flags(t *testing.T) {
	cmd := newDiffCmd()
	if v, _ := cmd.Flags().GetBool("json"); v {
		t.Error("json default should be false")
	}
	if v, _ := cmd.Flags().GetBool("baseline"); v {
		t.Error("baseline default should be false")
	}
}

func TestCobraInspect_Flags(t *testing.T) {
	cmd := newInspectCmd()
	if v, _ := cmd.Flags().GetBool("json"); v {
		t.Error("json default should be false")
	}
}

func TestCobraTriage_Flags(t *testing.T) {
	cmd := newTriageCmd()
	if v, _ := cmd.Flags().GetString("window"); v != "1m" {
		t.Errorf("window default = %q, want '1m'", v)
	}
	if v, _ := cmd.Flags().GetInt("top"); v != 50 {
		t.Errorf("top default = %d, want 50", v)
	}
	if v, _ := cmd.Flags().GetBool("html"); v {
		t.Error("html default should be false")
	}
}

func TestCobraGrep_Flags(t *testing.T) {
	cmd := newGrepCmd()
	if v, _ := cmd.Flags().GetBool("count"); v {
		t.Error("count default should be false")
	}
	if v, _ := cmd.Flags().GetBool("sort"); v {
		t.Error("sort default should be false")
	}
	if v, _ := cmd.Flags().GetString("format"); v != "json" {
		t.Errorf("format default = %q, want 'json'", v)
	}
	if v, _ := cmd.Flags().GetInt("context"); v != 0 {
		t.Errorf("context default = %d, want 0", v)
	}
}

func TestCobraCheck_Flags(t *testing.T) {
	cmd := newCheckCmd()
	if v, _ := cmd.Flags().GetBool("json"); v {
		t.Error("json default should be false")
	}
	if v, _ := cmd.Flags().GetString("namespace"); v != "" {
		t.Errorf("namespace default = %q, want empty", v)
	}
}

func TestCobraStatus_Flags(t *testing.T) {
	cmd := newStatusCmd()
	if v, _ := cmd.Flags().GetBool("json"); v {
		t.Error("json default should be false")
	}
	if v, _ := cmd.Flags().GetString("namespace"); v != "" {
		t.Errorf("namespace default = %q, want empty", v)
	}
}

func TestCobraSlice_Flags(t *testing.T) {
	cmd := newSliceCmd()
	if v, _ := cmd.Flags().GetBool("json"); v {
		t.Error("json default should be false")
	}
	if v, _ := cmd.Flags().GetString("from"); v != "" {
		t.Errorf("from default = %q, want empty", v)
	}
	if v, _ := cmd.Flags().GetString("to"); v != "" {
		t.Errorf("to default = %q, want empty", v)
	}
}

func TestCobraOpen_Flags(t *testing.T) {
	cmd := newOpenCmd()
	if v, _ := cmd.Flags().GetString("speed"); v != "1" {
		t.Errorf("speed default = %q, want '1'", v)
	}
	if v, _ := cmd.Flags().GetString("from"); v != "" {
		t.Errorf("from default = %q, want empty", v)
	}
}

func TestRunDiff_JSONSuccess(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	dirA := makeCaptureDir(t, sampleEntries(base))
	dirB := makeCaptureDir(t, sampleEntries(base.Add(5*time.Second)))

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newDiffCmd())

	restore := redirectOutput(t)
	defer restore()

	root.SetArgs([]string{"diff", dirA, dirB, "--json"})
	if err := root.Execute(); err != nil {
		t.Errorf("diff --json: %v", err)
	}
}

func TestCobraGC_DayAge(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	makeCaptureSub(t, root, "old-cap", now.Add(-72*time.Hour))

	cfg = config.Load()
	cmd := &cobra.Command{Use: "logtap"}
	cmd.AddCommand(newGCCmd())

	restore := redirectOutput(t)
	defer restore()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"gc", root, "--max-age", "1d", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Errorf("gc --max-age 1d: %v", err)
	}
}

func TestRunRecv_InClusterMissingImage(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "timeout")
	root.AddCommand(newRecvCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"recv", "--in-cluster"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for --in-cluster without --image")
	}
	if !strings.Contains(err.Error(), "--image required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunRecv_MissingDirCobra(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "timeout")
	root.AddCommand(newRecvCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"recv"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for missing --dir")
	}
	if !strings.Contains(err.Error(), "--dir is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunExport_WithFilters(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outPath := filepath.Join(t.TempDir(), "export.jsonl")

	restore := redirectOutput(t)
	defer restore()

	if err := runExport(dir, "jsonl", "", "", []string{"app=web"}, "hello", outPath, false); err != nil {
		t.Fatalf("runExport with filters: %v", err)
	}
}

func TestRunSlice_WithTimeFilter(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outDir := filepath.Join(t.TempDir(), "slice-time")

	restore := redirectOutput(t)
	defer restore()

	if err := runSlice(dir, "2025-01-15T10:00:00Z", "2025-01-15T10:00:03Z", nil, "", outDir); err != nil {
		t.Fatalf("runSlice with time: %v", err)
	}
}

func TestRunSlice_InvalidFrom(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outDir := filepath.Join(t.TempDir(), "slice-bad")

	err := runSlice(dir, "not-a-time", "", nil, "", outDir)
	if err == nil {
		t.Error("expected error for invalid --from")
	}
}

func TestRunSlice_InvalidTo(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outDir := filepath.Join(t.TempDir(), "slice-bad")

	err := runSlice(dir, "", "not-a-time", nil, "", outDir)
	if err == nil {
		t.Error("expected error for invalid --to")
	}
}

func TestPrintTextLine_NoLabels(t *testing.T) {
	restore := redirectOutput(t)
	defer restore()

	e := recv.LogEntry{
		Timestamp: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		Labels:    map[string]string{},
		Message:   "test message",
	}
	printTextLine(e, 10)
}

func TestCobraCompletion_InvalidShell(t *testing.T) {
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newCompletionCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"completion", "powershell"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for invalid shell")
	}
}

func TestCobraSnapshot_MissingOutput(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newSnapshotCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"snapshot", dir})
	if err := root.Execute(); err == nil {
		t.Error("expected error for missing --output")
	}
}

func TestBuildTestRoot_CatalogReport(t *testing.T) {
	cfg = config.Load()

	root := &cobra.Command{
		Use:   "logtap",
		Short: "Ephemeral log mirror for load testing",
	}
	root.PersistentFlags().StringVar(&timeoutStr, "timeout", "", "timeout")
	root.AddCommand(newCatalogCmd())
	root.AddCommand(newReportCmd())

	expected := map[string]bool{"catalog": false, "report": false}

	for _, c := range root.Commands() {
		if _, ok := expected[c.Name()]; ok {
			expected[c.Name()] = true
			if c.Use == "" {
				t.Errorf("%s: Use is empty", c.Name())
			}
			if c.Short == "" {
				t.Errorf("%s: Short is empty", c.Name())
			}
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestRunGC_NegativeAge(t *testing.T) {
	err := runGC("/tmp", "-1h", "", false, false)
	if err == nil {
		t.Error("expected error for negative --max-age")
	}
}

func TestRunGC_NegativeTotal(t *testing.T) {
	err := runGC("/tmp", "", "-100", false, false)
	if err == nil {
		t.Error("expected error for negative --max-total")
	}
}

func TestFormatBytes_EdgeCases(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{1023, "1023 B"},
		{1025, "1.0 KB"},
		{1048576 + 512*1024, "1.5 MB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRunExport_InvalidGrep(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outPath := filepath.Join(t.TempDir(), "export.jsonl")

	err := runExport(dir, "jsonl", "", "", nil, "[invalid(", outPath, false)
	if err == nil {
		t.Error("expected error for invalid grep")
	}
}

func TestCobraRecv_FlagDefaults(t *testing.T) {
	cmd := newRecvCmd()

	if v, _ := cmd.Flags().GetString("max-file"); v != "256MB" {
		t.Errorf("max-file default = %q, want '256MB'", v)
	}
	if v, _ := cmd.Flags().GetString("max-disk"); v != "50GB" {
		t.Errorf("max-disk default = %q, want '50GB'", v)
	}
	if v, _ := cmd.Flags().GetBool("compress"); !v {
		t.Error("compress default should be true")
	}
	if v, _ := cmd.Flags().GetBool("headless"); v {
		t.Error("headless default should be false")
	}
	if v, _ := cmd.Flags().GetInt("buffer"); v != 65536 {
		t.Errorf("buffer default = %d, want 65536", v)
	}
}

func TestCobraUntap_Flags(t *testing.T) {
	cmd := newUntapCmd()
	if v, _ := cmd.Flags().GetBool("all"); v {
		t.Error("all default should be false")
	}
	if v, _ := cmd.Flags().GetBool("dry-run"); v {
		t.Error("dry-run default should be false")
	}
	if v, _ := cmd.Flags().GetBool("force"); v {
		t.Error("force default should be false")
	}
	if v, _ := cmd.Flags().GetString("session"); v != "" {
		t.Errorf("session default = %q, want empty", v)
	}
}

func TestCobraTap_Flags(t *testing.T) {
	cmd := newTapCmd()
	if v, _ := cmd.Flags().GetBool("all"); v {
		t.Error("all default should be false")
	}
	if v, _ := cmd.Flags().GetBool("dry-run"); v {
		t.Error("dry-run default should be false")
	}
	if v, _ := cmd.Flags().GetBool("force"); v {
		t.Error("force default should be false")
	}
	if v, _ := cmd.Flags().GetBool("allow-prod"); v {
		t.Error("allow-prod default should be false")
	}
	if v, _ := cmd.Flags().GetBool("no-rollback"); v {
		t.Error("no-rollback default should be false")
	}
}

func TestCobraDiff_MissingArgs(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newDiffCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"diff", "/only/one"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for missing second arg")
	}
}

func TestCobraGrep_MissingArgs(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newGrepCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"grep"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for missing args")
	}
}

func TestCobraInspect_MissingArgs(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newInspectCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"inspect"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for missing args")
	}
}

func TestRunSlice_WithGrep(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))
	outDir := filepath.Join(t.TempDir(), "slice-grep")

	restore := redirectOutput(t)
	defer restore()

	if err := runSlice(dir, "", "", nil, "error", outDir); err != nil {
		t.Fatalf("runSlice with grep: %v", err)
	}
}

func TestRunGrep_InvalidPattern(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	err := runGrep("[invalid(", dir, "", "", nil, false, false, "json", 0)
	if err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

func TestCobraExport_MissingRequired(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newExportCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"export", "/tmp"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for missing required flags")
	}
}

func TestCobraMerge_MissingOut(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newMergeCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"merge", "/a", "/b"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for missing --out flag")
	}
}

func TestCobraReport_InvalidDir(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newReportCmd())

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"report", "/nonexistent/dir"})
	if err := root.Execute(); err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestCobraCatalog_RunDefault(t *testing.T) {
	cfg = config.Load()
	root := &cobra.Command{Use: "logtap"}
	root.AddCommand(newCatalogCmd())

	restore := redirectOutput(t)
	defer restore()

	// catalog with no args defaults to "."
	root.SetArgs([]string{"catalog"})
	// May succeed or fail depending on working dir, but shouldn't panic
	_ = root.Execute()
}

func TestCheckResult_JSON(t *testing.T) {
	result := &checkResult{
		Candidates: nil,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "{}") && !strings.Contains(string(data), "null") {
		t.Errorf("unexpected JSON: %s", data)
	}
}
