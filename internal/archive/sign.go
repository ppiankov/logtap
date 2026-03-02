package archive

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const manifestFile = "manifest.sha256"

// SignResult holds the output of signing a capture directory.
type SignResult struct {
	Dir      string       `json:"dir"`
	RootHash string       `json:"root_hash"`
	Files    []FileDigest `json:"files"`
	SignedAt time.Time    `json:"signed_at"`
}

// FileDigest is a single file's SHA256 hash and size.
type FileDigest struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// VerifyResult holds the output of verifying a signed capture.
type VerifyResult struct {
	Dir        string         `json:"dir"`
	RootHash   string         `json:"root_hash"`
	Valid      bool           `json:"valid"`
	Mismatches []FileMismatch `json:"mismatches,omitempty"`
	Missing    []string       `json:"missing,omitempty"`
	Extra      []string       `json:"extra,omitempty"`
}

// FileMismatch records a hash mismatch between manifest and actual file.
type FileMismatch struct {
	File     string `json:"file"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// Sign computes SHA256 hashes for all files in a capture directory
// and writes a manifest.sha256 file. Returns the signing result.
func Sign(dir string) (*SignResult, error) {
	if _, err := os.Stat(filepath.Join(dir, "metadata.json")); err != nil {
		return nil, fmt.Errorf("not a valid capture directory: %w", err)
	}

	files, err := captureFiles(dir)
	if err != nil {
		return nil, err
	}

	var digests []FileDigest
	for _, name := range files {
		h, size, err := hashFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", name, err)
		}
		digests = append(digests, FileDigest{File: name, SHA256: h, Bytes: size})
	}

	rootHash := computeRootHash(digests)

	if err := writeManifest(dir, digests); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	return &SignResult{
		Dir:      dir,
		RootHash: rootHash,
		Files:    digests,
		SignedAt: time.Now().UTC(),
	}, nil
}

// Verify checks the integrity of a signed capture directory against its manifest.
func Verify(dir string) (*VerifyResult, error) {
	manifestPath := filepath.Join(dir, manifestFile)
	expected, err := readManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	result := &VerifyResult{
		Dir:   dir,
		Valid: true,
	}

	// Hash all expected files and check for mismatches/missing.
	for _, entry := range expected {
		actual, _, err := hashFile(filepath.Join(dir, entry.File))
		if err != nil {
			if os.IsNotExist(err) {
				result.Missing = append(result.Missing, entry.File)
				result.Valid = false
				continue
			}
			return nil, fmt.Errorf("hash %s: %w", entry.File, err)
		}
		if actual != entry.SHA256 {
			result.Mismatches = append(result.Mismatches, FileMismatch{
				File:     entry.File,
				Expected: entry.SHA256,
				Actual:   actual,
			})
			result.Valid = false
		}
	}

	// Check for extra files not in the manifest.
	expectedSet := make(map[string]bool, len(expected))
	for _, e := range expected {
		expectedSet[e.File] = true
	}
	currentFiles, err := captureFiles(dir)
	if err != nil {
		return nil, err
	}
	for _, name := range currentFiles {
		if !expectedSet[name] {
			result.Extra = append(result.Extra, name)
			result.Valid = false
		}
	}

	result.RootHash = computeRootHash(expected)

	return result, nil
}

// captureFiles returns sorted filenames of all regular files in dir,
// excluding manifest.sha256 itself.
func captureFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == manifestFile {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// hashFile computes the SHA256 hex digest and byte size of a file.
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// computeRootHash computes SHA256 of the concatenated manifest lines.
func computeRootHash(digests []FileDigest) string {
	h := sha256.New()
	for _, d := range digests {
		_, _ = fmt.Fprintf(h, "%s  %s\n", d.SHA256, d.File)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeManifest writes the sha256sum-compatible manifest file.
func writeManifest(dir string, digests []FileDigest) error {
	path := filepath.Join(dir, manifestFile)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	for _, d := range digests {
		if _, err := fmt.Fprintf(f, "%s  %s\n", d.SHA256, d.File); err != nil {
			return err
		}
	}
	return nil
}

// readManifest parses a sha256sum-format manifest file.
func readManifest(path string) ([]FileDigest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var digests []FileDigest
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			continue
		}
		digests = append(digests, FileDigest{
			File:   parts[1],
			SHA256: parts[0],
		})
	}
	return digests, scanner.Err()
}

// WriteJSON writes the sign result as indented JSON.
func (r *SignResult) WriteJSON(w io.Writer) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w)
	return err
}

// WriteText writes a human-readable signing summary.
func (r *SignResult) WriteText(w io.Writer) {
	_, _ = fmt.Fprintf(w, "Signed %s (%d files)\n", r.Dir, len(r.Files))
	_, _ = fmt.Fprintf(w, "Root hash: %s\n", r.RootHash)
	_, _ = fmt.Fprintf(w, "Manifest:  %s\n", manifestFile)
	for _, f := range r.Files {
		_, _ = fmt.Fprintf(w, "  %s  %s (%d bytes)\n", f.SHA256[:12], f.File, f.Bytes)
	}
}

// WriteJSON writes the verify result as indented JSON.
func (r *VerifyResult) WriteJSON(w io.Writer) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w)
	return err
}

// WriteText writes a human-readable verification summary.
func (r *VerifyResult) WriteText(w io.Writer) {
	if r.Valid {
		_, _ = fmt.Fprintf(w, "OK: %s integrity verified\n", r.Dir)
		return
	}
	_, _ = fmt.Fprintf(w, "FAIL: %s integrity check failed\n", r.Dir)
	for _, m := range r.Mismatches {
		_, _ = fmt.Fprintf(w, "  MISMATCH  %s\n", m.File)
		_, _ = fmt.Fprintf(w, "    expected: %s\n", m.Expected)
		_, _ = fmt.Fprintf(w, "    actual:   %s\n", m.Actual)
	}
	for _, name := range r.Missing {
		_, _ = fmt.Fprintf(w, "  MISSING   %s\n", name)
	}
	for _, name := range r.Extra {
		_, _ = fmt.Fprintf(w, "  EXTRA     %s\n", name)
	}
}
