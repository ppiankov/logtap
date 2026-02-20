package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestCLIError_Error(t *testing.T) {
	e := NewUsageError("bad flag")
	if e.Error() != "bad flag" {
		t.Errorf("Error() = %q, want %q", e.Error(), "bad flag")
	}
}

func TestCLIError_Categories(t *testing.T) {
	tests := []struct {
		name     string
		err      *CLIError
		wantCode int
		wantType string
		wantRecv bool
	}{
		{"usage", NewUsageError("bad"), ExitUsage, "invalid_args", false},
		{"not_found", NewNotFoundError("missing"), ExitNotFound, "not_found", false},
		{"permission", NewPermissionError("denied"), ExitPermission, "permission", false},
		{"network", NewNetworkError("timeout"), ExitNetwork, "network", true},
		{"internal", NewInternalError("panic"), ExitInternal, "internal", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.wantCode {
				t.Errorf("Code = %d, want %d", tt.err.Code, tt.wantCode)
			}
			if tt.err.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", tt.err.Type, tt.wantType)
			}
			if tt.err.Recover != tt.wantRecv {
				t.Errorf("Recover = %v, want %v", tt.err.Recover, tt.wantRecv)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	if got := ExitCode(nil); got != ExitOK {
		t.Errorf("ExitCode(nil) = %d, want %d", got, ExitOK)
	}
	if got := ExitCode(NewNotFoundError("x")); got != ExitNotFound {
		t.Errorf("ExitCode(not_found) = %d, want %d", got, ExitNotFound)
	}
	if got := ExitCode(errors.New("plain error")); got != ExitInternal {
		t.Errorf("ExitCode(plain) = %d, want %d", got, ExitInternal)
	}
}

func TestFormatError_JSON(t *testing.T) {
	var buf bytes.Buffer

	// CLIError
	FormatError(&buf, NewNotFoundError("no such capture"), true)
	var parsed CLIError
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.Type != "not_found" {
		t.Errorf("type = %q, want %q", parsed.Type, "not_found")
	}
	if parsed.Code != ExitNotFound {
		t.Errorf("code = %d, want %d", parsed.Code, ExitNotFound)
	}

	// plain error wraps as internal
	buf.Reset()
	FormatError(&buf, errors.New("something broke"), true)
	var parsed2 CLIError
	if err := json.Unmarshal(buf.Bytes(), &parsed2); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed2.Type != "internal" {
		t.Errorf("type = %q, want %q", parsed2.Type, "internal")
	}
}

func TestFormatError_Text(t *testing.T) {
	var buf bytes.Buffer
	FormatError(&buf, NewUsageError("bad flag"), false)
	want := "error: bad flag\n"
	if buf.String() != want {
		t.Errorf("text = %q, want %q", buf.String(), want)
	}
}

func TestFormatError_Nil(t *testing.T) {
	var buf bytes.Buffer
	FormatError(&buf, nil, true)
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil error, got %q", buf.String())
	}
}
