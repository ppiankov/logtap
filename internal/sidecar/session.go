package sidecar

import (
	"crypto/rand"
	"fmt"
)

// GenerateSessionID returns a unique session identifier in the format
// "lt-XXXXXXXXXXXXXXXX" where X is 16 lowercase hex characters (8 bytes)
// from crypto/rand, providing 64 bits of entropy.
func GenerateSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return fmt.Sprintf("lt-%016x", b), nil
}
