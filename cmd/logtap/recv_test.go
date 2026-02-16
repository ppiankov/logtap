package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ppiankov/logtap/internal/recv"
)

func TestRunHeadless_Shutdown(t *testing.T) {
	restore := redirectOutput(t)
	defer restore()

	var buf bytes.Buffer
	writer := recv.NewWriter(1, &buf, nil)
	errCh := make(chan error, 1)
	errCh <- errors.New("http: Server closed")

	called := false
	shutdown := func() {
		called = true
		writer.Close()
	}

	if err := runHeadless(":0", t.TempDir(), writer, errCh, shutdown); err != nil {
		t.Fatalf("runHeadless: %v", err)
	}
	if !called {
		t.Fatal("expected shutdown to be called")
	}
}

func TestRunRecv_InvalidListen(t *testing.T) {
	restore := redirectOutput(t)
	defer restore()

	dir := t.TempDir()
	err := runRecv("invalid", dir, "1KB", "1MB", false, "true", "", 8, true, "", "", nil, "")
	if err == nil {
		t.Fatal("expected error for invalid listen address")
	}
}
