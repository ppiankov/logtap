package logtypes

import (
	"time"
)

// LogEntry represents a single parsed log line.
type LogEntry struct {
	Timestamp time.Time         `json:"ts"`
	Labels    map[string]string `json:"labels,omitempty"`
	Message   string            `json:"msg"`
}