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

	re := regexp.MustCompile(`^lt-[0-9a-f]{4}$`)
	if !re.MatchString(id) {
		t.Errorf("session ID %q does not match lt-XXXX format", id)
	}
}

func TestGenerateSessionID_Unique(t *testing.T) {
	a, err := GenerateSessionID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("two session IDs are identical: %s", a)
	}
}
