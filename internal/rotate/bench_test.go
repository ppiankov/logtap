package rotate

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

func BenchmarkRotatorWrite(b *testing.B) {
	dir := b.TempDir()
	r, err := New(Config{
		Dir:      dir,
		MaxFile:  100 << 20, // 100MB — avoid rotation during bench
		Compress: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	entry := recv.LogEntry{
		Timestamp: time.Now(),
		Labels:    map[string]string{"app": "api", "env": "prod"},
		Message:   "GET /api/v1/users 200 OK latency=12ms",
	}
	data, _ := json.Marshal(entry)
	data = append(data, '\n')

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Write(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRotatorWriteWithTracking(b *testing.B) {
	dir := b.TempDir()
	r, err := New(Config{
		Dir:      dir,
		MaxFile:  100 << 20,
		Compress: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	ts := time.Now()
	labels := map[string]string{"app": "api", "env": "prod"}
	entry := recv.LogEntry{
		Timestamp: ts,
		Labels:    labels,
		Message:   "GET /api/v1/users 200 OK latency=12ms",
	}
	data, _ := json.Marshal(entry)
	data = append(data, '\n')

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Write(data); err != nil {
			b.Fatal(err)
		}
		r.TrackLine(ts.Add(time.Duration(i)*time.Millisecond), labels)
	}
}

func BenchmarkRotatorWithRotation(b *testing.B) {
	dir := b.TempDir()
	r, err := New(Config{
		Dir:      dir,
		MaxFile:  1 << 20, // 1MB — force frequent rotation
		Compress: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	entry := recv.LogEntry{
		Timestamp: time.Now(),
		Labels:    map[string]string{"app": "api"},
		Message:   fmt.Sprintf("log line with padding: %0200d", 0), // ~200B per line
	}
	data, _ := json.Marshal(entry)
	data = append(data, '\n')

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Write(data); err != nil {
			b.Fatal(err)
		}
	}
}
