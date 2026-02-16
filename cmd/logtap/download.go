package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/ppiankov/logtap/internal/cloud"
	"github.com/ppiankov/logtap/internal/recv"
)

func newDownloadCmd() *cobra.Command {
	var (
		outDir      string
		concurrency int
	)

	cmd := &cobra.Command{
		Use:   "download <cloud-url>",
		Short: "Download capture from cloud storage",
		Long:  "Download a capture directory from S3 or GCS, reconstructing local directory structure.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if outDir == "" {
				return fmt.Errorf("--out is required")
			}
			return runDownload(cmd.Context(), args[0], outDir, concurrency)
		},
	}

	cmd.Flags().StringVarP(&outDir, "out", "o", "", "output directory (required)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "number of parallel downloads")

	return cmd
}

func runDownload(ctx context.Context, fromURL, outDir string, concurrency int) error {
	scheme, bucket, prefix, err := cloud.ParseURL(fromURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	backend, err := cloud.NewBackend(ctx, scheme, bucket)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", scheme, err)
	}

	return downloadCapture(ctx, backend, prefix, outDir, concurrency)
}

func downloadCapture(ctx context.Context, backend cloud.Backend, prefix, outDir string, concurrency int) error {
	objects, err := backend.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}
	if len(objects) == 0 {
		return fmt.Errorf("no objects found under prefix %q", prefix)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	var totalBytes int64
	for _, obj := range objects {
		totalBytes += obj.Size
	}

	var (
		downloadedFiles atomic.Int64
		downloadedBytes atomic.Int64
		sem             = make(chan struct{}, concurrency)
		wg              sync.WaitGroup
		firstErr        error
		errOnce         sync.Once
	)

	for _, obj := range objects {
		sem <- struct{}{}
		wg.Add(1)
		go func(obj cloud.ObjectInfo) {
			defer wg.Done()
			defer func() { <-sem }()

			relPath := stripPrefix(obj.Key, prefix)
			localPath := filepath.Join(outDir, filepath.FromSlash(relPath))

			if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
				errOnce.Do(func() { firstErr = fmt.Errorf("mkdir for %s: %w", relPath, err) })
				return
			}

			f, err := os.Create(localPath)
			if err != nil {
				errOnce.Do(func() { firstErr = fmt.Errorf("create %s: %w", relPath, err) })
				return
			}

			if err := backend.Download(ctx, obj.Key, f); err != nil {
				_ = f.Close()
				errOnce.Do(func() { firstErr = fmt.Errorf("download %s: %w", relPath, err) })
				return
			}
			_ = f.Close()

			n := downloadedFiles.Add(1)
			b := downloadedBytes.Add(obj.Size)
			fmt.Fprintf(os.Stderr, "\rDownloading: %d/%d files (%s / %s)",
				n, int64(len(objects)), archive.FormatBytes(b), archive.FormatBytes(totalBytes))
		}(obj)
	}

	wg.Wait()
	fmt.Fprintln(os.Stderr)

	if firstErr != nil {
		return firstErr
	}

	if _, err := recv.ReadMetadata(outDir); err != nil {
		return fmt.Errorf("downloaded capture invalid (missing or corrupt metadata.json): %w", err)
	}

	fmt.Fprintf(os.Stderr, "Downloaded %d files (%s) to %s\n",
		len(objects), archive.FormatBytes(totalBytes), outDir)
	return nil
}

func stripPrefix(key, prefix string) string {
	if prefix == "" {
		return key
	}
	trimmed := strings.TrimPrefix(key, prefix)
	trimmed = strings.TrimPrefix(trimmed, "/")
	return trimmed
}
