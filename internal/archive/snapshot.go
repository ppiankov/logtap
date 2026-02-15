package archive

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/ppiankov/logtap/internal/recv"
)

// Pack creates a tar.zst archive from a capture directory.
func Pack(src, dst string) error {
	// Validate source is a capture directory
	metaPath := filepath.Join(src, "metadata.json")
	if _, err := os.Stat(metaPath); err != nil {
		return fmt.Errorf("not a capture directory (missing metadata.json): %w", err)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}

	zw, err := zstd.NewWriter(out)
	if err != nil {
		_ = out.Close()
		return fmt.Errorf("create zstd writer: %w", err)
	}

	tw := tar.NewWriter(zw)

	walkErr := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("file info header %s: %w", rel, err)
		}
		header.Name = rel

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("write header %s: %w", rel, err)
		}

		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", rel, err)
		}
		_, copyErr := io.Copy(tw, f)
		_ = f.Close()
		return copyErr
	})

	// Close in reverse order: tar → zstd → file
	if twErr := tw.Close(); twErr != nil && walkErr == nil {
		walkErr = twErr
	}
	if zwErr := zw.Close(); zwErr != nil && walkErr == nil {
		walkErr = zwErr
	}
	if outErr := out.Close(); outErr != nil && walkErr == nil {
		walkErr = outErr
	}

	return walkErr
}

// Unpack extracts a tar.zst archive to a directory and validates the capture.
func Unpack(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("create zstd reader: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		// Sanitize path to prevent directory traversal
		clean := filepath.Clean(header.Name)
		if strings.HasPrefix(clean, "..") {
			return fmt.Errorf("invalid path in archive: %s", header.Name)
		}
		target := filepath.Join(dst, clean)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create dir %s: %w", clean, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := extractFile(target, tr); err != nil {
				return fmt.Errorf("write file %s: %w", clean, err)
			}
		}
	}

	// Validate: metadata.json must exist and be parseable
	metaPath := filepath.Join(dst, "metadata.json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("extracted archive missing metadata.json: %w", err)
	}
	var meta recv.Metadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return fmt.Errorf("invalid metadata.json: %w", err)
	}

	// Validate: index.jsonl must exist
	indexPath := filepath.Join(dst, "index.jsonl")
	if _, err := os.Stat(indexPath); err != nil {
		return fmt.Errorf("extracted archive missing index.jsonl: %w", err)
	}

	return nil
}

func extractFile(target string, r io.Reader) error {
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, r)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
