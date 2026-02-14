package sidecar

import (
	"crypto/rand"
	"fmt"
)

// GenerateSessionID returns a unique session identifier in the format "lt-XXXX"
// where XXXX is 4 lowercase hex characters from crypto/rand.
func GenerateSessionID() (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return fmt.Sprintf("lt-%04x", b), nil
}
