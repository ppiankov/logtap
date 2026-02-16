package main

import (
	"strings"
	"testing"
	"time"
)

func TestRunOpen_InvalidSpeed(t *testing.T) {
	dir := makeCaptureDir(t, sampleEntries(time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)))

	err := runOpen(dir, "not-a-speed", "", "", nil, "")
	if err == nil {
		t.Fatal("expected error for invalid speed")
	}
	if !strings.Contains(err.Error(), "invalid --speed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
