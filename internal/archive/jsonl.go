package archive

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/ppiankov/logtap/internal/recv"
)

type jsonlWriter struct {
	file *os.File
	buf  *bufio.Writer
	enc  *json.Encoder
}

func newJSONLWriter(path string) (*jsonlWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	buf := bufio.NewWriter(f)
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)

	return &jsonlWriter{file: f, buf: buf, enc: enc}, nil
}

func (w *jsonlWriter) Write(e recv.LogEntry) error {
	return w.enc.Encode(e)
}

func (w *jsonlWriter) Close() error {
	if err := w.buf.Flush(); err != nil {
		_ = w.file.Close()
		return err
	}
	return w.file.Close()
}
