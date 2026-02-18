package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Metadata represents the structure of metadata.json
type Metadata struct {
	Version    int                    `json:"version"`
	Format     string                 `json:"format"`
	Started    time.Time              `json:"started"`
	Stopped    time.Time              `json:"stopped"`
	TotalLines int64                  `json:"total_lines"`
	TotalBytes int64                  `json:"total_bytes"`
	LabelsSeen []string               `json:"labels_seen"`
	Redaction  map[string]interface{} `json:"redaction"`
}

// NewMetadata creates a new Metadata instance with default values.
func NewMetadata() *Metadata {
	return &Metadata{}
}

// ReadMetadata reads the metadata.json file from the specified directory.
func ReadMetadata(dir string) (*Metadata, error) {
	path := filepath.Join(dir, "metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// WriteMetadata writes the Metadata struct to metadata.json in the specified directory.
func WriteMetadata(dir string, meta *Metadata) error {
	path := filepath.Join(dir, "metadata.json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}