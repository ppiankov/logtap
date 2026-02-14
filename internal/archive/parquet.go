package archive

import (
	"os"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"

	"github.com/ppiankov/logtap/internal/recv"
)

const parquetBatchSize = 50000

// parquetEntry is the Parquet schema struct.
type parquetEntry struct {
	Ts     int64             `parquet:"ts,timestamp(nanosecond)"`
	Labels map[string]string `parquet:"labels"`
	Msg    string            `parquet:"msg"`
}

type parquetWriter struct {
	file   *os.File
	writer *parquet.GenericWriter[parquetEntry]
	batch  []parquetEntry
}

func newParquetWriter(path string) (*parquetWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	w := parquet.NewGenericWriter[parquetEntry](f,
		parquet.Compression(&zstd.Codec{}),
	)

	return &parquetWriter{
		file:   f,
		writer: w,
		batch:  make([]parquetEntry, 0, parquetBatchSize),
	}, nil
}

func (w *parquetWriter) Write(e recv.LogEntry) error {
	w.batch = append(w.batch, parquetEntry{
		Ts:     e.Timestamp.UnixNano(),
		Labels: e.Labels,
		Msg:    e.Message,
	})
	if len(w.batch) >= parquetBatchSize {
		return w.flush()
	}
	return nil
}

func (w *parquetWriter) flush() error {
	if len(w.batch) == 0 {
		return nil
	}
	_, err := w.writer.Write(w.batch)
	w.batch = w.batch[:0]
	return err
}

func (w *parquetWriter) Close() error {
	if err := w.flush(); err != nil {
		_ = w.writer.Close()
		_ = w.file.Close()
		return err
	}
	if err := w.writer.Close(); err != nil {
		_ = w.file.Close()
		return err
	}
	return w.file.Close()
}
