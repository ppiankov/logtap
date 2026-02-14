package recv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Metadata records session-level information for a capture directory.
type Metadata struct {
	Version    int            `json:"version"`
	Format     string         `json:"format"`
	Started    time.Time      `json:"started"`
	Stopped    time.Time      `json:"stopped,omitempty"`
	TotalLines int64          `json:"total_lines"`
	TotalBytes int64          `json:"total_bytes"`
	LabelsSeen []string       `json:"labels_seen"`
	Redaction  *RedactionInfo `json:"redaction,omitempty"`
}

// RedactionInfo records which redaction patterns were active.
type RedactionInfo struct {
	Enabled  bool     `json:"enabled"`
	Patterns []string `json:"patterns"`
}

// WriteMetadata writes metadata.json to the given directory.
func WriteMetadata(dir string, meta *Metadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644)
}

// ReadMetadata reads metadata.json from the given directory.
func ReadMetadata(dir string) (*Metadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return nil, err
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}
	return &meta, nil
}
