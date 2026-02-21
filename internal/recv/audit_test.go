package recv

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditLog_WriteAndRead(t *testing.T) {
	dir := t.TempDir()

	a, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatal(err)
	}

	a.Log(AuditEntry{Event: "server_started"})
	a.Log(AuditEntry{Event: "push_received", RemoteIP: "10.0.0.1", Lines: 42, Bytes: 1024})
	a.Log(AuditEntry{Event: "server_stopped"})

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	var entries []AuditEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e AuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		entries = append(entries, e)
	}

	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	if entries[0].Event != "server_started" {
		t.Errorf("event[0] = %q, want %q", entries[0].Event, "server_started")
	}
	if entries[1].RemoteIP != "10.0.0.1" {
		t.Errorf("remote_ip = %q, want %q", entries[1].RemoteIP, "10.0.0.1")
	}
	if entries[1].Lines != 42 {
		t.Errorf("lines = %d, want 42", entries[1].Lines)
	}
	if entries[2].Event != "server_stopped" {
		t.Errorf("event[2] = %q, want %q", entries[2].Event, "server_stopped")
	}
	// verify timestamps are set
	for i, e := range entries {
		if e.Timestamp.IsZero() {
			t.Errorf("entry[%d] timestamp is zero", i)
		}
	}
}

func TestAuditLog_FilePermissions(t *testing.T) {
	dir := t.TempDir()

	a, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.Log(AuditEntry{Event: "test"})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("audit.jsonl permissions = %o, want 0600", perm)
	}
}

func TestAuditLog_NilSafe(t *testing.T) {
	var a *AuditLogger
	// should not panic
	a.Log(AuditEntry{Event: "test"})
	if err := a.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}
