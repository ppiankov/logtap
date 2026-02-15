package recv

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func BenchmarkWriter(b *testing.B) {
	w := NewWriter(65536, io.Discard, nil)
	defer w.Close()

	entry := LogEntry{
		Timestamp: time.Now(),
		Labels:    map[string]string{"app": "api", "env": "prod", "pod": "api-abc-123"},
		Message:   "GET /api/v1/users 200 OK latency=12ms",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Send(entry)
	}
}

func BenchmarkRedactNoMatch(b *testing.B) {
	r, err := NewRedactor(nil)
	if err != nil {
		b.Fatal(err)
	}
	msg := "GET /api/v1/users 200 OK latency=12ms request_id=abc-123-def"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Redact(msg)
	}
}

func BenchmarkRedactWithMatches(b *testing.B) {
	r, err := NewRedactor(nil)
	if err != nil {
		b.Fatal(err)
	}
	// Fake PII values to exercise redaction patterns (email, IP, bearer-style token)
	msg := "User test@example.com from 192.168.1.100 with token BEARER-fake-test-token-value logged in"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Redact(msg)
	}
}

func BenchmarkRedactLargeMessage(b *testing.B) {
	r, err := NewRedactor(nil)
	if err != nil {
		b.Fatal(err)
	}
	// 1KB message with scattered PII
	parts := make([]string, 20)
	for i := range parts {
		if i%5 == 0 {
			parts[i] = fmt.Sprintf("user%d@example.com accessed resource %d", i, i)
		} else {
			parts[i] = fmt.Sprintf("processing request %d with latency %dms status=200", i, i*3)
		}
	}
	msg := strings.Join(parts, " | ")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Redact(msg)
	}
}

func BenchmarkLogEntryMarshal(b *testing.B) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Labels:    map[string]string{"app": "api", "env": "prod", "pod": "api-abc-123", "namespace": "default"},
		Message:   "GET /api/v1/users 200 OK latency=12ms request_id=abc-123-def-456-ghi",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(entry)
		_ = data
	}
}
