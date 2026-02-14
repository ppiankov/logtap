package recv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry records a single auditable event.
type AuditEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Event     string    `json:"event"`
	RemoteIP  string    `json:"remote_ip,omitempty"`
	Lines     int       `json:"lines,omitempty"`
	Bytes     int       `json:"bytes,omitempty"`
}

// AuditLogger writes append-only JSONL audit records.
type AuditLogger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

// NewAuditLogger creates an audit logger writing to <dir>/audit.jsonl.
func NewAuditLogger(dir string) (*AuditLogger, error) {
	path := filepath.Join(dir, "audit.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &AuditLogger{file: f, enc: json.NewEncoder(f)}, nil
}

// Log writes an audit entry. Safe to call from multiple goroutines.
// If a is nil, the call is a no-op.
func (a *AuditLogger) Log(entry AuditEntry) {
	if a == nil {
		return
	}
	entry.Timestamp = time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.enc.Encode(entry)
}

// Close flushes and closes the audit log file.
func (a *AuditLogger) Close() error {
	if a == nil {
		return nil
	}
	return a.file.Close()
}
