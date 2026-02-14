package archive

import (
	"fmt"

	"github.com/ppiankov/logtap/internal/recv"
)

// ExportFormat identifies the output format.
type ExportFormat string

const (
	FormatParquet ExportFormat = "parquet"
	FormatCSV     ExportFormat = "csv"
	FormatJSONL   ExportFormat = "jsonl"
)

// ExportProgress reports progress during export.
type ExportProgress struct {
	Written int64
	Total   int64 // source total lines from index
}

// ExportWriter writes log entries to an output format.
type ExportWriter interface {
	Write(recv.LogEntry) error
	Close() error
}

// Export reads filtered entries from src and writes to dst in the given format.
func Export(src, dst string, format ExportFormat, filter *Filter, progress func(ExportProgress)) error {
	reader, err := NewReader(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	totalLines := reader.TotalLines()

	writer, err := newExportWriter(dst, format)
	if err != nil {
		return fmt.Errorf("create writer: %w", err)
	}

	var written int64
	_, err = reader.Scan(filter, func(e recv.LogEntry) bool {
		if werr := writer.Write(e); werr != nil {
			return true // skip write errors, continue scanning
		}
		written++

		if progress != nil && written%10000 == 0 {
			progress(ExportProgress{
				Written: written,
				Total:   totalLines,
			})
		}
		return true
	})
	if err != nil {
		_ = writer.Close()
		return fmt.Errorf("scan source: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("close writer: %w", err)
	}

	// final progress
	if progress != nil {
		progress(ExportProgress{
			Written: written,
			Total:   totalLines,
		})
	}

	return nil
}

func newExportWriter(path string, format ExportFormat) (ExportWriter, error) {
	switch format {
	case FormatParquet:
		return newParquetWriter(path)
	case FormatCSV:
		return newCSVWriter(path)
	case FormatJSONL:
		return newJSONLWriter(path)
	default:
		return nil, fmt.Errorf("unsupported format: %q", format)
	}
}
