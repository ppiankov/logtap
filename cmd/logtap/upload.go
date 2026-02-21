package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/ppiankov/logtap/internal/cloud"
	"github.com/ppiankov/logtap/internal/recv"
)

func newUploadCmd() *cobra.Command {
	var (
		to          string
		concurrency int
		jsonOutput  bool
	)

	cmd := &cobra.Command{
		Use:   "upload <capture-dir>",
		Short: "Upload capture to cloud storage",
		Long:  "Upload a capture directory to S3 or GCS, preserving directory structure.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if to == "" {
				return fmt.Errorf("--to is required")
			}
			return runUpload(cmd.Context(), args[0], to, concurrency, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "destination URL (s3://bucket/prefix or gs://bucket/prefix)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "number of parallel uploads")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output summary as JSON")

	return cmd
}

func runUpload(ctx context.Context, dir, toURL string, concurrency int, jsonOutput bool) error {
	if _, err := recv.ReadMetadata(dir); err != nil {
		return fmt.Errorf("not a valid capture directory: %w", err)
	}

	scheme, bucket, prefix, err := cloud.ParseURL(toURL)
	if err != nil {
		return fmt.Errorf("invalid --to: %w", err)
	}

	backend, err := cloud.NewBackend(ctx, scheme, bucket)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", scheme, err)
	}

	stats, err := uploadCapture(ctx, dir, backend, prefix, concurrency)
	if err != nil {
		return err
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"source":      dir,
			"destination": toURL,
			"files":       stats.files,
			"bytes":       stats.bytes,
		})
	}

	_, _ = fmt.Fprintf(os.Stderr, "Destination: %s\n", toURL)
	return nil
}

type uploadFile struct {
	path    string
	relPath string
	size    int64
}

type uploadStats struct {
	files int
	bytes int64
}

func uploadCapture(ctx context.Context, dir string, backend cloud.Backend, prefix string, concurrency int) (uploadStats, error) {
	var files []uploadFile
	var totalBytes int64

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, uploadFile{path: path, relPath: filepath.ToSlash(rel), size: info.Size()})
		totalBytes += info.Size()
		return nil
	})
	if err != nil {
		return uploadStats{}, fmt.Errorf("walk capture dir: %w", err)
	}

	if len(files) == 0 {
		return uploadStats{}, fmt.Errorf("no files found in %s", dir)
	}

	var (
		uploadedFiles atomic.Int64
		uploadedBytes atomic.Int64
		sem           = make(chan struct{}, concurrency)
		wg            sync.WaitGroup
		firstErr      error
		errOnce       sync.Once
	)

	for _, uf := range files {
		sem <- struct{}{}
		wg.Add(1)
		go func(uf uploadFile) {
			defer wg.Done()
			defer func() { <-sem }()

			key := uf.relPath
			if prefix != "" {
				key = prefix + "/" + key
			}

			f, err := os.Open(uf.path)
			if err != nil {
				errOnce.Do(func() { firstErr = fmt.Errorf("open %s: %w", uf.relPath, err) })
				return
			}
			defer func() { _ = f.Close() }()

			if err := backend.Upload(ctx, key, f, uf.size); err != nil {
				errOnce.Do(func() { firstErr = fmt.Errorf("upload %s: %w", uf.relPath, err) })
				return
			}

			n := uploadedFiles.Add(1)
			b := uploadedBytes.Add(uf.size)
			_, _ = fmt.Fprintf(os.Stderr, "\rUploading: %d/%d files (%s / %s)",
				n, int64(len(files)), archive.FormatBytes(b), archive.FormatBytes(totalBytes))
		}(uf)
	}

	wg.Wait()
	_, _ = fmt.Fprintln(os.Stderr)

	if firstErr != nil {
		return uploadStats{}, firstErr
	}

	_, _ = fmt.Fprintf(os.Stderr, "Uploaded %d files (%s)\n",
		len(files), archive.FormatBytes(totalBytes))
	return uploadStats{files: len(files), bytes: totalBytes}, nil
}
