package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

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
		share       bool
		expiry      time.Duration
		force       bool
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
			return runUpload(cmd.Context(), args[0], to, concurrency, jsonOutput, share, expiry, force)
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "destination URL (s3://bucket/prefix or gs://bucket/prefix)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "number of parallel uploads")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output summary as JSON")
	cmd.Flags().BoolVar(&share, "share", false, "generate presigned URLs after upload")
	cmd.Flags().DurationVar(&expiry, "expiry", 24*time.Hour, "presigned URL expiry (max 168h)")
	cmd.Flags().BoolVar(&force, "force", false, "allow sharing unredacted captures")

	return cmd
}

func runUpload(ctx context.Context, dir, toURL string, concurrency int, jsonOutput, share bool, expiry time.Duration, force bool) error {
	meta, err := recv.ReadMetadata(dir)
	if err != nil {
		return fmt.Errorf("not a valid capture directory: %w", err)
	}

	// safety gate: refuse to share unredacted captures without --force
	if share && meta.Redaction == nil && !force {
		return fmt.Errorf("capture not redacted — use --force to share unredacted logs")
	}

	if share && expiry > 168*time.Hour {
		return fmt.Errorf("--expiry exceeds maximum of 168h (7 days)")
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

	if share {
		if meta.Redaction == nil {
			_, _ = fmt.Fprintln(os.Stderr, "WARNING: sharing unredacted capture — PII may be exposed")
		}
		return generateShareURLs(ctx, backend, prefix, stats, toURL, expiry, jsonOutput)
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

func generateShareURLs(ctx context.Context, backend cloud.Backend, prefix string, stats uploadStats, toURL string, expiry time.Duration, jsonOutput bool) error {
	objects, err := backend.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("list uploaded files: %w", err)
	}

	urls := make(map[string]string, len(objects))
	for _, obj := range objects {
		u, err := backend.ShareURL(ctx, obj.Key, expiry)
		if err != nil {
			return fmt.Errorf("generate share URL for %s: %w", obj.Key, err)
		}
		urls[obj.Key] = u
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"destination": toURL,
			"files":       stats.files,
			"bytes":       stats.bytes,
			"expires":     time.Now().Add(expiry).UTC().Format(time.RFC3339),
			"share_urls":  urls,
		})
	}

	_, _ = fmt.Fprintf(os.Stderr, "Shared %d files (expires in %s)\n", len(urls), expiry)
	// print metadata.json URL if present, otherwise first URL
	for key, u := range urls {
		if filepath.Base(key) == "metadata.json" {
			_, _ = fmt.Fprintf(os.Stderr, "Metadata: %s\n", u)
			break
		}
	}
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
