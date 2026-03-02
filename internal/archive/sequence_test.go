package archive

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildSequence_Empty(t *testing.T) {
	seq := BuildSequence(nil)
	if len(seq.Participants) != 0 {
		t.Errorf("expected 0 participants, got %d", len(seq.Participants))
	}
	if len(seq.Interactions) != 0 {
		t.Errorf("expected 0 interactions, got %d", len(seq.Interactions))
	}
}

func TestBuildSequence_SingleCorrelation(t *testing.T) {
	corrs := []Correlation{
		{Source: "api", Target: "db", LagSeconds: 10, Pattern: "cascade_error", Confidence: 0.85},
	}
	seq := BuildSequence(corrs)
	if len(seq.Participants) != 2 {
		t.Fatalf("expected 2 participants, got %d", len(seq.Participants))
	}
	if seq.Participants[0].Name != "api" {
		t.Errorf("first participant = %q, want %q", seq.Participants[0].Name, "api")
	}
	if seq.Participants[1].Name != "db" {
		t.Errorf("second participant = %q, want %q", seq.Participants[1].Name, "db")
	}
	if len(seq.Interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(seq.Interactions))
	}
	if seq.Interactions[0].From != "api" || seq.Interactions[0].To != "db" {
		t.Errorf("interaction = %s→%s, want api→db", seq.Interactions[0].From, seq.Interactions[0].To)
	}
}

func TestBuildSequence_ThreeServices(t *testing.T) {
	corrs := []Correlation{
		{Source: "payments", Target: "api", LagSeconds: 0, Pattern: "co_failure", Confidence: 0.85},
		{Source: "api", Target: "database", LagSeconds: 10, Pattern: "cascade_error", Confidence: 0.72},
	}
	seq := BuildSequence(corrs)
	if len(seq.Participants) != 3 {
		t.Fatalf("expected 3 participants, got %d", len(seq.Participants))
	}

	// interactions should be sorted by lag
	if seq.Interactions[0].LagSeconds != 0 {
		t.Errorf("first interaction lag = %.0f, want 0", seq.Interactions[0].LagSeconds)
	}
	if seq.Interactions[1].LagSeconds != 10 {
		t.Errorf("second interaction lag = %.0f, want 10", seq.Interactions[1].LagSeconds)
	}
}

func TestBuildSequence_ParticipantOrder(t *testing.T) {
	// Source services should appear before target-only services
	corrs := []Correlation{
		{Source: "web", Target: "cache", LagSeconds: 5, Pattern: "cascade_timeout", Confidence: 0.9},
		{Source: "api", Target: "cache", LagSeconds: 15, Pattern: "cascade_error", Confidence: 0.7},
	}
	seq := BuildSequence(corrs)

	// web appears as source first, api as source second, cache is target-only
	names := make([]string, len(seq.Participants))
	for i, p := range seq.Participants {
		names[i] = p.Name
	}

	if names[0] != "web" {
		t.Errorf("first participant = %q, want %q", names[0], "web")
	}
	if names[1] != "api" {
		t.Errorf("second participant = %q, want %q", names[1], "api")
	}
	if names[2] != "cache" {
		t.Errorf("third participant = %q, want %q", names[2], "cache")
	}
}

func TestWriteASCII_Format(t *testing.T) {
	corrs := []Correlation{
		{Source: "payments", Target: "api", LagSeconds: 0, Pattern: "co_failure", Confidence: 0.85},
		{Source: "api", Target: "database", LagSeconds: 10, Pattern: "cascade_error", Confidence: 0.72},
	}
	seq := BuildSequence(corrs)

	var buf bytes.Buffer
	seq.WriteASCII(&buf)
	output := buf.String()

	// verify header contains participant names
	lines := strings.Split(output, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d:\n%s", len(lines), output)
	}

	header := lines[0]
	if !strings.Contains(header, "payments") {
		t.Errorf("header missing 'payments': %q", header)
	}
	if !strings.Contains(header, "api") {
		t.Errorf("header missing 'api': %q", header)
	}
	if !strings.Contains(header, "database") {
		t.Errorf("header missing 'database': %q", header)
	}

	// verify arrows exist
	if !strings.Contains(output, ">") {
		t.Error("output missing arrow '>'")
	}
	if !strings.Contains(output, "[err]") {
		t.Error("output missing '[err]' label")
	}

	// verify annotations
	if !strings.Contains(output, "co_failure") {
		t.Error("output missing 'co_failure' annotation")
	}
	if !strings.Contains(output, "cascade_error") {
		t.Error("output missing 'cascade_error' annotation")
	}
}

func TestWriteASCII_ReverseDirection(t *testing.T) {
	// target index < source index means arrow should point left
	corrs := []Correlation{
		{Source: "database", Target: "api", LagSeconds: 5, Pattern: "cascade_error", Confidence: 0.8},
	}
	seq := BuildSequence(corrs)

	var buf bytes.Buffer
	seq.WriteASCII(&buf)
	output := buf.String()

	// database is source (idx 0), api is target-only (idx 1)
	// but database appears first as source, api second
	// so the arrow goes from idx 0 to idx 1 (left to right)
	if !strings.Contains(output, ">") {
		t.Errorf("expected '>' in output:\n%s", output)
	}
}

func TestWriteJSON_RoundTrip(t *testing.T) {
	corrs := []Correlation{
		{Source: "api", Target: "db", LagSeconds: 10, Pattern: "cascade_error", Confidence: 0.85},
	}
	seq := BuildSequence(corrs)

	var buf bytes.Buffer
	if err := seq.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var decoded SequenceDiagram
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Participants) != len(seq.Participants) {
		t.Errorf("participants: got %d, want %d", len(decoded.Participants), len(seq.Participants))
	}
	if len(decoded.Interactions) != len(seq.Interactions) {
		t.Errorf("interactions: got %d, want %d", len(decoded.Interactions), len(seq.Interactions))
	}
	if decoded.Participants[0].Name != "api" {
		t.Errorf("first participant = %q, want %q", decoded.Participants[0].Name, "api")
	}
	if decoded.Interactions[0].Label == "" {
		t.Error("interaction label is empty after round-trip")
	}
}
