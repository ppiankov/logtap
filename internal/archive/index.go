package archive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Index represents the structure of index.jsonl
type Index struct {
	Entries []IndexEntry `json:"entries"`
}

// IndexEntry represents a single entry in index.jsonl
type IndexEntry struct {
	File  string                     `json:"file"`
	From  time.Time                  `json:"from"`
	To    time.Time                  `json:"to"`
	Lines int64                      `json:"lines"`
	Bytes int64                      `json:"bytes"`
	Labels map[string]map[string]int `json:"labels"`
}

// NewIndex creates a new Index instance.
func NewIndex() *Index {
	return &Index{Entries: []IndexEntry{}}
}

// ReadIndex reads the index.jsonl file from the specified directory.
func ReadIndex(dir string) (*Index, error) {
	path := filepath.Join(dir, "index.jsonl")
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []IndexEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry IndexEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, fmt.Errorf("failed to parse index entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &Index{Entries: entries}, nil
}

// WriteIndex writes the Index struct to index.jsonl in the specified directory.
func WriteIndex(dir string, index *Index) error {
	path := filepath.Join(dir, "index.jsonl")
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, entry := range index.Entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return writer.Flush()
}
