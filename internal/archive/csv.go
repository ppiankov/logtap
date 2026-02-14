package archive

import (
	"encoding/csv"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

type csvWriter struct {
	file *os.File
	w    *csv.Writer
}

func newCSVWriter(path string) (*csvWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	w := csv.NewWriter(f)
	if err := w.Write([]string{"ts", "labels", "msg"}); err != nil {
		_ = f.Close()
		return nil, err
	}

	return &csvWriter{file: f, w: w}, nil
}

func (w *csvWriter) Write(e recv.LogEntry) error {
	return w.w.Write([]string{
		e.Timestamp.Format(time.RFC3339Nano),
		flattenLabels(e.Labels),
		e.Message,
	})
}

func (w *csvWriter) Close() error {
	w.w.Flush()
	if err := w.w.Error(); err != nil {
		_ = w.file.Close()
		return err
	}
	return w.file.Close()
}

// flattenLabels produces a deterministic "key=val;key=val" string sorted by key.
func flattenLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}
