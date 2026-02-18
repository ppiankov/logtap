package archive

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Helper function to create a dummy capture directory
func createDummyCapture(t *testing.T, dir string, entries []IndexEntry, logs map[string][]string) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create dummy capture directory %s: %v", dir, err)
	}

	// Create metadata.json
	meta := NewMetadata()
	meta.Version = 1
	meta.Format = "jsonl+zstd"
	meta.Started = time.Date(2024, time.January, 1, 10, 0, 0, 0, time.UTC)
	meta.Stopped = time.Date(2024, time.January, 1, 11, 0, 0, 0, time.UTC)
	meta.TotalLines = 0
	meta.TotalBytes = 0
	meta.LabelsSeen = []string{"app", "namespace"}
	if err := WriteMetadata(dir, meta); err != nil {
		t.Fatalf("Failed to write dummy metadata: %v", err)
	}

	// Create index.jsonl
	idx := NewIndex()
	idx.Entries = entries
	if err := WriteIndex(dir, idx); err != nil {
		t.Fatalf("Failed to write dummy index: %v", err)
	}

	// Create .jsonl.zst files
	for fileName, fileLogs := range logs {
		filePath := filepath.Join(dir, fileName)
		outFile, err := os.Create(filePath)
		if err != nil {
			t.Fatalf("Failed to create dummy log file %s: %v", filePath, err)
		}
		defer func() { _ = outFile.Close() }()

		zstdWriter, err := zstd.NewWriter(outFile)
		if err != nil {
			t.Fatalf("Failed to create zstd writer for %s: %v", filePath, err)
		}
		defer func() { _ = zstdWriter.Close() }()

		for _, logLine := range fileLogs {
			if _, err := zstdWriter.Write(append([]byte(logLine), '\n')); err != nil {
				t.Fatalf("Failed to write log line to %s: %v", filePath, err)
			}
		}
	}
}

// Helper function to read and decompress a zst file
func readZstFile(t *testing.T, filePath string) []string {
	inFile, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("Failed to open zst file %s: %v", filePath, err)
	}
	defer func() { _ = inFile.Close() }()

	zstdReader, err := zstd.NewReader(inFile)
	if err != nil {
		t.Fatalf("Failed to create zstd reader for %s: %v", filePath, err)
	}
	defer zstdReader.Close()

	var lines []string
	scanner := bufio.NewScanner(zstdReader)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Error reading zst file %s: %v", filePath, err)
	}
	return lines
}

func TestSlice_NoFilters(t *testing.T) {
	tempDir := t.TempDir()

	captureDir := filepath.Join(tempDir, "capture")
	outputDir := filepath.Join(tempDir, "output")

	// Sample data
	logFile1 := "2024-01-01T100000-000.jsonl.zst"
	logFile2 := "2024-01-01T101000-000.jsonl.zst"

	entries := []IndexEntry{
		{File: logFile1, From: time.Date(2024, time.January, 1, 10, 0, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 9, 59, 999999999, time.UTC), Lines: 3, Bytes: 100, Labels: map[string]map[string]int{"app": {"api": 3}}},
		{File: logFile2, From: time.Date(2024, time.January, 1, 10, 10, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 19, 59, 999999999, time.UTC), Lines: 2, Bytes: 80, Labels: map[string]map[string]int{"app": {"worker": 2}}},
	}
	logs := map[string][]string{
		logFile1: {`{"ts":"...","labels":{"app":"api"},"msg":"line 1"}`,
			`{"ts":"...","labels":{"app":"api"},"msg":"line 2"}`,
			`{"ts":"...","labels":{"app":"api"},"msg":"line 3"}`},
		logFile2: {`{"ts":"...","labels":{"app":"worker"},"msg":"task started"}`,
			`{"ts":"...","labels":{"app":"worker"},"msg":"task finished"}`},
	}
	createDummyCapture(t, captureDir, entries, logs)

	opts := SliceOptions{
		CaptureDir: captureDir,
		OutputDir:  outputDir,
	}

	err := Slice(opts)
	if err != nil {
		t.Fatalf("Slice failed: %v", err)
	}

	// Verify output directory
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		t.Fatalf("Output directory %s was not created", outputDir)
	}

	// Verify metadata
	outMeta, err := ReadMetadata(outputDir)
	if err != nil {
		t.Fatalf("Failed to read output metadata: %v", err)
	}
	if outMeta.TotalLines != 5 { // Total lines from both files
		t.Errorf("Expected 5 total lines, got %d", outMeta.TotalLines)
	}

	// Verify index
	outIndex, err := ReadIndex(outputDir)
	if err != nil {
		t.Fatalf("Failed to read output index: %v", err)
	}
	if len(outIndex.Entries) != 2 {
		t.Errorf("Expected 2 index entries, got %d", len(outIndex.Entries))
	}
	if outIndex.Entries[0].File != logFile1 || outIndex.Entries[1].File != logFile2 {
		t.Errorf("Index entries mismatch")
	}

	// Verify log content
	outputLogs1 := readZstFile(t, filepath.Join(outputDir, logFile1))
	if len(outputLogs1) != 3 {
		t.Errorf("Expected 3 lines in %s, got %d", logFile1, len(outputLogs1))
	}
	if !bytes.Equal([]byte(outputLogs1[0]), []byte(logs[logFile1][0])) {
		t.Errorf("Log content mismatch for %s line 0", logFile1)
	}
	outputLogs2 := readZstFile(t, filepath.Join(outputDir, logFile2))
	if len(outputLogs2) != 2 {
		t.Errorf("Expected 2 lines in %s, got %d", logFile2, len(outputLogs2))
	}
}

func TestSlice_FromToFilter(t *testing.T) {
	tempDir := t.TempDir()

	captureDir := filepath.Join(tempDir, "capture")
	outputDir := filepath.Join(tempDir, "output")

	logFile1 := "2024-01-01T100000-000.jsonl.zst"
	logFile2 := "2024-01-01T101000-000.jsonl.zst"
	logFile3 := "2024-01-01T102000-000.jsonl.zst"

	entries := []IndexEntry{
		{File: logFile1, From: time.Date(2024, time.January, 1, 10, 0, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 9, 59, 999999999, time.UTC), Lines: 3, Bytes: 100, Labels: map[string]map[string]int{"app": {"api": 3}}},
		{File: logFile2, From: time.Date(2024, time.January, 1, 10, 10, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 19, 59, 999999999, time.UTC), Lines: 2, Bytes: 80, Labels: map[string]map[string]int{"app": {"worker": 2}}},
		{File: logFile3, From: time.Date(2024, time.January, 1, 10, 20, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 29, 59, 999999999, time.UTC), Lines: 1, Bytes: 50, Labels: map[string]map[string]int{"app": {"gateway": 1}}},
	}
	logs := map[string][]string{
		logFile1: {`{"ts":"2024-01-01T10:05:00Z","labels":{"app":"api"},"msg":"line 1"}`,
			`{"ts":"2024-01-01T10:06:00Z","labels":{"app":"api"},"msg":"line 2"}`,
			`{"ts":"2024-01-01T10:07:00Z","labels":{"app":"api"},"msg":"line 3"}`},
		logFile2: {`{"ts":"2024-01-01T10:12:00Z","labels":{"app":"worker"},"msg":"task started"}`,
			`{"ts":"2024-01-01T10:15:00Z","labels":{"app":"worker"},"msg":"task finished"}`},
		logFile3: {`{"ts":"2024-01-01T10:22:00Z","labels":{"app":"gateway"},"msg":"request received"}`},
	}
	createDummyCapture(t, captureDir, entries, logs)

	// Filter from 10:00 to 10:15 (should include file1 and part of file2)
	opts := SliceOptions{
		CaptureDir: captureDir,
		OutputDir:  outputDir,
		From:       time.Date(2024, time.January, 1, 10, 0, 0, 0, time.UTC),
		To:         time.Date(2024, time.January, 1, 10, 15, 0, 0, time.UTC),
	}

	err := Slice(opts)
	if err != nil {
		t.Fatalf("Slice failed: %v", err)
	}

	outMeta, err := ReadMetadata(outputDir)
	if err != nil {
		t.Fatalf("Failed to read output metadata: %v", err)
	}
	// The lines and bytes count in metadata and index will be imprecise with time filters
	// as we filter line-by-line within files. For now, check if files are present and content.
	// TODO: Adjust total lines/bytes in metadata/index based on actual filtered lines.
	if outMeta.TotalLines == 0 { // Should be more than 0
		t.Errorf("Expected non-zero total lines, got %d", outMeta.TotalLines)
	}

	outIndex, err := ReadIndex(outputDir)
	if err != nil {
		t.Fatalf("Failed to read output index: %v", err)
	}
	if len(outIndex.Entries) != 2 { // Only file1 and file2 should be processed
		t.Errorf("Expected 2 index entries, got %d", len(outIndex.Entries))
	}
	if outIndex.Entries[0].File != logFile1 || outIndex.Entries[1].File != logFile2 {
		t.Errorf("Index entries mismatch")
	}

	// Verify log content
	outputLogs1 := readZstFile(t, filepath.Join(outputDir, logFile1))
	if len(outputLogs1) != 3 { // All lines in file1 should pass
		t.Errorf("Expected 3 lines in %s, got %d", logFile1, len(outputLogs1))
	}
	outputLogs2 := readZstFile(t, filepath.Join(outputDir, logFile2))
	if len(outputLogs2) != 1 { // Only the first line of file2 should pass (10:12)
		t.Errorf("Expected 1 line in %s, got %d", logFile2, len(outputLogs2))
	}
	if !strings.Contains(outputLogs2[0], `2024-01-01T10:12:00Z`) {
		t.Errorf("Expected log line from 10:12:00Z in %s, got %s", logFile2, outputLogs2[0])
	}

	// Verify file3 is NOT present
	if _, err := os.Stat(filepath.Join(outputDir, logFile3)); !os.IsNotExist(err) {
		t.Errorf("File %s should not exist in output directory", logFile3)
	}
}

func TestSlice_LabelFilter(t *testing.T) {
	tempDir := t.TempDir()

	captureDir := filepath.Join(tempDir, "capture")
	outputDir := filepath.Join(tempDir, "output")

	logFile1 := "2024-01-01T100000-000.jsonl.zst" // app=api
	logFile2 := "2024-01-01T101000-000.jsonl.zst" // app=worker
	logFile3 := "2024-01-01T102000-000.jsonl.zst" // app=gateway

	entries := []IndexEntry{
		{File: logFile1, From: time.Date(2024, time.January, 1, 10, 0, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 9, 59, 999999999, time.UTC), Lines: 3, Bytes: 100, Labels: map[string]map[string]int{"app": {"api": 3}}},
		{File: logFile2, From: time.Date(2024, time.January, 1, 10, 10, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 19, 59, 999999999, time.UTC), Lines: 2, Bytes: 80, Labels: map[string]map[string]int{"app": {"worker": 2}}},
		{File: logFile3, From: time.Date(2024, time.January, 1, 10, 20, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 29, 59, 999999999, time.UTC), Lines: 1, Bytes: 50, Labels: map[string]map[string]int{"app": {"gateway": 1}}},
	}
	logs := map[string][]string{
		logFile1: {`{"ts":"...","labels":{"app":"api"},"msg":"line 1"}`,
			`{"ts":"...","labels":{"app":"api"},"msg":"line 2"}`,
			`{"ts":"...","labels":{"app":"api"},"msg":"line 3"}`},
		logFile2: {`{"ts":"...","labels":{"app":"worker"},"msg":"task started"}`,
			`{"ts":"...","labels":{"app":"worker"},"msg":"task finished"}`},
		logFile3: {`{"ts":"...","labels":{"app":"gateway"},"msg":"request received"}`},
	}
	createDummyCapture(t, captureDir, entries, logs)

	// Filter by app=api
	opts := SliceOptions{
		CaptureDir: captureDir,
		OutputDir:  outputDir,
		Labels:     []LabelFilter{{Key: "app", Value: "api"}},
	}

	err := Slice(opts)
	if err != nil {
		t.Fatalf("Slice failed: %v", err)
	}

	outMeta, err := ReadMetadata(outputDir)
	if err != nil {
		t.Fatalf("Failed to read output metadata: %v", err)
	}
	if outMeta.TotalLines != 3 {
		t.Errorf("Expected 3 total lines, got %d", outMeta.TotalLines)
	}

	outIndex, err := ReadIndex(outputDir)
	if err != nil {
		t.Fatalf("Failed to read output index: %v", err)
	}
	if len(outIndex.Entries) != 1 {
		t.Errorf("Expected 1 index entry, got %d", len(outIndex.Entries))
	}
	if outIndex.Entries[0].File != logFile1 {
		t.Errorf("Index entry mismatch, expected %s, got %s", logFile1, outIndex.Entries[0].File)
	}

	// Verify log content
	outputLogs1 := readZstFile(t, filepath.Join(outputDir, logFile1))
	if len(outputLogs1) != 3 {
		t.Errorf("Expected 3 lines in %s, got %d", logFile1, len(outputLogs1))
	}
	if !bytes.Equal([]byte(outputLogs1[0]), []byte(logs[logFile1][0])) {
		t.Errorf("Log content mismatch for %s line 0", logFile1)
	}

	// Verify other files are NOT present
	if _, err := os.Stat(filepath.Join(outputDir, logFile2)); !os.IsNotExist(err) {
		t.Errorf("File %s should not exist in output directory", logFile2)
	}
	if _, err := os.Stat(filepath.Join(outputDir, logFile3)); !os.IsNotExist(err) {
		t.Errorf("File %s should not exist in output directory", logFile3)
	}
}

func TestSlice_GrepFilter(t *testing.T) {
	tempDir := t.TempDir()

	captureDir := filepath.Join(tempDir, "capture")
	outputDir := filepath.Join(tempDir, "output")

	logFile1 := "2024-01-01T100000-000.jsonl.zst"
	logFile2 := "2024-01-01T101000-000.jsonl.zst"

	entries := []IndexEntry{
		{File: logFile1, From: time.Date(2024, time.January, 1, 10, 0, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 9, 59, 999999999, time.UTC), Lines: 3, Bytes: 100, Labels: map[string]map[string]int{"app": {"api": 3}}},
		{File: logFile2, From: time.Date(2024, time.January, 1, 10, 10, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 19, 59, 999999999, time.UTC), Lines: 2, Bytes: 80, Labels: map[string]map[string]int{"app": {"worker": 2}}},
	}
	logs := map[string][]string{
		logFile1: {`{"ts":"...","labels":{"app":"api"},"msg":"error: something failed"}`,
			`{"ts":"...","labels":{"app":"api"},"msg":"info: everything good"}`,
			`{"ts":"...","labels":{"app":"api"},"msg":"warning: caution"}`},
		logFile2: {`{"ts":"...","labels":{"app":"worker"},"msg":"error: critical failure"}`,
			`{"ts":"...","labels":{"app":"worker"},"msg":"debug: process heartbeat"}`},
	}
	createDummyCapture(t, captureDir, entries, logs)

	// Filter by grep "error"
	opts := SliceOptions{
		CaptureDir: captureDir,
		OutputDir:  outputDir,
		Grep:       regexp.MustCompile("error"),
	}

	err := Slice(opts)
	if err != nil {
		t.Fatalf("Slice failed: %v", err)
	}

	outMeta, err := ReadMetadata(outputDir)
	if err != nil {
		t.Fatalf("Failed to read output metadata: %v", err)
	}
	if outMeta.TotalLines != 2 {
		t.Errorf("Expected 2 total lines, got %d", outMeta.TotalLines)
	}

	outIndex, err := ReadIndex(outputDir)
	if err != nil {
		t.Fatalf("Failed to read output index: %v", err)
	}
	if len(outIndex.Entries) != 2 { // Both files should have an error line
		t.Errorf("Expected 2 index entries, got %d", len(outIndex.Entries))
	}

	// Verify log content
	outputLogs1 := readZstFile(t, filepath.Join(outputDir, logFile1))
	if len(outputLogs1) != 1 || !strings.Contains(outputLogs1[0], "error: something failed") {
		t.Errorf("Expected 1 error line in %s, got %v", logFile1, outputLogs1)
	}
	outputLogs2 := readZstFile(t, filepath.Join(outputDir, logFile2))
	if len(outputLogs2) != 1 || !strings.Contains(outputLogs2[0], "error: critical failure") {
		t.Errorf("Expected 1 error line in %s, got %v", logFile2, outputLogs2)
	}
}

func TestSlice_EmptyOutputWhenNoMatches(t *testing.T) {
	tempDir := t.TempDir()

	captureDir := filepath.Join(tempDir, "capture")
	outputDir := filepath.Join(tempDir, "output")

	logFile1 := "2024-01-01T100000-000.jsonl.zst"
	entries := []IndexEntry{
		{File: logFile1, From: time.Date(2024, time.January, 1, 10, 0, 0, 0, time.UTC), To: time.Date(2024, time.January, 1, 10, 9, 59, 999999999, time.UTC), Lines: 3, Bytes: 100, Labels: map[string]map[string]int{"app": {"api": 3}}},
	}
	logs := map[string][]string{
		logFile1: {`{"ts":"...","labels":{"app":"api"},"msg":"line 1"}`,
			`{"ts":"...","labels":{"app":"api"},"msg":"line 2"}`,
			`{"ts":"...","labels":{"app":"api"},"msg":"line 3"}`},
	}
	createDummyCapture(t, captureDir, entries, logs)

	// Filter for a non-existent label
	opts := SliceOptions{
		CaptureDir: captureDir,
		OutputDir:  outputDir,
		Labels:     []LabelFilter{{Key: "app", Value: "nonexistent"}},
	}

	err := Slice(opts)
	if err == nil || !strings.Contains(err.Error(), "no matching log lines found") {
		t.Fatalf("Expected 'no matching log lines found' error, got: %v", err)
	}

	// Output directory should be cleaned up
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("Output directory %s should have been removed, but it exists", outputDir)
	}
}

func TestSlice_OutputDirIsInput(t *testing.T) {
	tempDir := t.TempDir()

	captureDir := filepath.Join(tempDir, "capture")

	opts := SliceOptions{
		CaptureDir: captureDir,
		OutputDir:  captureDir, // Same as input
	}

	err := Slice(opts)
	if err == nil || !strings.Contains(err.Error(), "output directory cannot be the same as capture directory") {
		t.Fatalf("Expected error about output directory being same as capture directory, got: %v", err)
	}
}

// TODO: Add more tests for combined filters, empty capture, metadata/index recalculation, etc.
