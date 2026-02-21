package sidecar

import (
	"regexp"
	"testing"
)

func TestGenerateSessionID_Format(t *testing.T) {
	id, err := GenerateSessionID()
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`^lt-[0-9a-f]{16}$`)
	if !re.MatchString(id) {
		t.Errorf("session ID %q does not match lt-XXXXXXXXXXXXXXXX format", id)
	}
	if len(id) != 19 { // "lt-" (3) + 16 hex chars = 19
		t.Errorf("session ID length = %d, want 19", len(id))
	}
}

func TestGenerateSessionID_Unique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id, err := GenerateSessionID()
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("collision on iteration %d: %s", i, id)
		}
		seen[id] = true
	}
}
