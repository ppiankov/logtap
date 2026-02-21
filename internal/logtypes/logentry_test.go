package logtypes

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLogEntry_JSONRoundTrip(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	entry := LogEntry{
		Timestamp: ts,
		Labels:    map[string]string{"app": "web", "env": "dev"},
		Message:   "hello world",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded LogEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !decoded.Timestamp.Equal(ts) {
		t.Fatalf("timestamp mismatch: got %v, want %v", decoded.Timestamp, ts)
	}
	if decoded.Message != "hello world" {
		t.Fatalf("message mismatch: got %q", decoded.Message)
	}
	if decoded.Labels["app"] != "web" || decoded.Labels["env"] != "dev" {
		t.Fatalf("labels mismatch: got %v", decoded.Labels)
	}
}

func TestLogEntry_JSON_OmitsEmptyLabels(t *testing.T) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Message:   "no labels",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	if _, ok := m["labels"]; ok {
		t.Fatal("expected labels to be omitted when nil")
	}
}
