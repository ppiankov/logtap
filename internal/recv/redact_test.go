package recv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreditCardRedaction(t *testing.T) {
	r, err := NewRedactor([]string{"credit_card"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"visa", "card: 4111111111111111", "card: [REDACTED:cc]"},
		{"mastercard", "pay with 5500000000000004", "pay with [REDACTED:cc]"},
		{"amex", "amex 378282246310005", "amex [REDACTED:cc]"},
		{"with spaces", "card 4111 1111 1111 1111 end", "card [REDACTED:cc] end"},
		{"with dashes", "card 4111-1111-1111-1111 end", "card [REDACTED:cc] end"},
		{"random digits no luhn", "number 1234567890123456", "number 1234567890123456"},
		{"too short", "num 12345678", "num 12345678"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Redact(tt.input)
			if got != tt.expect {
				t.Errorf("got %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestEmailRedaction(t *testing.T) {
	r, err := NewRedactor([]string{"email"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"basic", "user test@example.com logged in", "user [REDACTED:email] logged in"},
		{"start of line", "admin@corp.io is admin", "[REDACTED:email] is admin"},
		{"end of line", "contact: user@domain.org", "contact: [REDACTED:email]"},
		{"plus addressing", "user+tag@example.com here", "[REDACTED:email] here"},
		{"not email", "no email here", "no email here"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Redact(tt.input)
			if got != tt.expect {
				t.Errorf("got %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestJWTRedaction(t *testing.T) {
	r, err := NewRedactor([]string{"jwt"})
	if err != nil {
		t.Fatal(err)
	}

	// construct JWT-shaped test fixture dynamically to avoid secret detection
	jwtParts := []string{"eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiIxMjM0NTY3ODkwIn0", "fakesignaturevalue"}
	jwt := jwtParts[0] + "." + jwtParts[1] + "." + jwtParts[2]
	got := r.Redact("token: " + jwt + " end")
	if got != "token: [REDACTED:jwt] end" {
		t.Errorf("got %q", got)
	}
}

func TestBearerRedaction(t *testing.T) {
	r, err := NewRedactor([]string{"bearer"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		input  string
		expect string
	}{
		{"Bearer abc123_token", "[REDACTED:bearer]"},
		{"Authorization: Bearer xyz789", "[REDACTED:bearer]"},
	}

	for _, tt := range tests {
		got := r.Redact(tt.input)
		if got != tt.expect {
			t.Errorf("input %q: got %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestIPv4Redaction(t *testing.T) {
	r, err := NewRedactor([]string{"ip_v4"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"basic", "from 192.168.1.1 to server", "from [REDACTED:ip] to server"},
		{"localhost", "connect 127.0.0.1:8080", "connect [REDACTED:ip]:8080"},
		{"no false positive on version", "version 1.2.3 released", "version 1.2.3 released"},
		{"high octets", "addr 255.255.255.0 mask", "addr [REDACTED:ip] mask"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Redact(tt.input)
			if got != tt.expect {
				t.Errorf("got %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestSSNRedaction(t *testing.T) {
	r, err := NewRedactor([]string{"ssn"})
	if err != nil {
		t.Fatal(err)
	}

	got := r.Redact("ssn: 123-45-6789 end")
	if got != "ssn: [REDACTED:ssn] end" {
		t.Errorf("got %q", got)
	}
}

func TestPhoneRedaction(t *testing.T) {
	r, err := NewRedactor([]string{"phone"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		input  string
		expect string
	}{
		{"call (555) 123-4567", "call [REDACTED:phone]"},
		{"phone +1-555-123-4567", "phone [REDACTED:phone]"},
		{"dial 5551234567", "dial [REDACTED:phone]"},
	}

	for _, tt := range tests {
		got := r.Redact(tt.input)
		if got != tt.expect {
			t.Errorf("input %q: got %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestCustomPatterns(t *testing.T) {
	r, err := NewRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}

	yamlContent := `- name: api_key
  pattern: "(?i)api[_-]?key[=: ]+[A-Za-z0-9]{16,}"
  replacement: "[REDACTED:apikey]"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "patterns.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := r.LoadCustomPatterns(path); err != nil {
		t.Fatal(err)
	}

	got := r.Redact("api_key=abcdef1234567890xx end")
	if got != "[REDACTED:apikey] end" {
		t.Errorf("got %q", got)
	}
}

func TestCombinedRedaction(t *testing.T) {
	r, err := NewRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}

	input := "user test@example.com paid with 4111111111111111 from 10.0.0.1"
	got := r.Redact(input)

	for _, marker := range []string{"[REDACTED:email]", "[REDACTED:cc]", "[REDACTED:ip]"} {
		if !contains(got, marker) {
			t.Errorf("expected %s in output: %q", marker, got)
		}
	}
}

func TestCleanMessagePassthrough(t *testing.T) {
	r, err := NewRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}

	msg := "2024-01-01 INFO application started successfully"
	got := r.Redact(msg)
	if got != msg {
		t.Errorf("clean message modified: %q -> %q", msg, got)
	}
}

func TestParseRedactFlag(t *testing.T) {
	tests := []struct {
		val     string
		enabled bool
		names   []string
	}{
		{"", false, nil},
		{"true", true, nil},
		{"credit_card,email", true, []string{"credit_card", "email"}},
	}

	for _, tt := range tests {
		enabled, names := ParseRedactFlag(tt.val)
		if enabled != tt.enabled {
			t.Errorf("val=%q: enabled=%v, want %v", tt.val, enabled, tt.enabled)
		}
		if len(names) != len(tt.names) {
			t.Errorf("val=%q: names=%v, want %v", tt.val, names, tt.names)
		}
	}
}

func TestUnknownPattern(t *testing.T) {
	_, err := NewRedactor([]string{"nonexistent"})
	if err == nil {
		t.Error("expected error for unknown pattern")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
