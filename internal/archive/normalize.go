package archive

import (
	"regexp"
	"strings"
)

// normalizer replaces variable tokens in log messages to extract stable error signatures.
var normalizers = []struct {
	re   *regexp.Regexp
	repl string
}{
	// UUIDs: 8-4-4-4-12 hex
	{regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`), "<UUID>"},
	// ISO timestamps: 2024-01-15T10:32:01 (with optional fractional seconds and timezone)
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[.\d]*Z?`), "<TS>"},
	// IPv4 addresses
	{regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`), "<IP>"},
	// Hex strings: 0x1a2b3c
	{regexp.MustCompile(`0x[0-9a-fA-F]+`), "<HEX>"},
	// Durations: 230ms, 1.5s, 30m, 2h
	{regexp.MustCompile(`\b\d+\.?\d*(?:ms|us|µs|ns|s|m|h)\b`), "<DUR>"},
	// Large numbers: 4+ digits (preserves short codes like HTTP 200, 500)
	{regexp.MustCompile(`\b\d{4,}\b`), "<N>"},
}

// NormalizeMessage replaces variable tokens (UUIDs, IPs, timestamps, etc.) with
// placeholders to produce a stable signature for grouping similar messages.
func NormalizeMessage(msg string) string {
	for _, n := range normalizers {
		msg = n.re.ReplaceAllString(msg, n.repl)
	}
	return msg
}

// errorKeywords are checked case-insensitively against log messages.
var errorKeywords = []string{
	"error",
	"panic",
	"fatal",
	"exception",
	"fail",
	"refused",
	"timeout",
	"oomkilled",
	"crashloopbackoff",
	"segfault",
	"x509",
	"deadline exceeded",
}

// IsError returns true if the message contains common error-indicating keywords.
func IsError(msg string) bool {
	lower := strings.ToLower(msg)
	for _, kw := range errorKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
