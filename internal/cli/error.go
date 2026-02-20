package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Exit codes for agent retry logic.
const (
	ExitOK         = 0
	ExitInternal   = 1
	ExitUsage      = 2
	ExitNotFound   = 3
	ExitPermission = 4
	ExitNetwork    = 5
	ExitFindings   = 6
)

// CLIError is a structured error with a category for agent consumption.
type CLIError struct {
	Code    int    `json:"exit_code"`
	Type    string `json:"error"`
	Message string `json:"message"`
	Recover bool   `json:"recoverable"`
}

func (e *CLIError) Error() string {
	return e.Message
}

// Unwrap returns nil — CLIError is a leaf error.
func (e *CLIError) Unwrap() error { return nil }

// NewUsageError creates an error for invalid arguments.
func NewUsageError(msg string) *CLIError {
	return &CLIError{Code: ExitUsage, Type: "invalid_args", Message: msg}
}

// NewNotFoundError creates an error for missing resources.
func NewNotFoundError(msg string) *CLIError {
	return &CLIError{Code: ExitNotFound, Type: "not_found", Message: msg}
}

// NewPermissionError creates an error for access denied.
func NewPermissionError(msg string) *CLIError {
	return &CLIError{Code: ExitPermission, Type: "permission", Message: msg}
}

// NewNetworkError creates a recoverable network error.
func NewNetworkError(msg string) *CLIError {
	return &CLIError{Code: ExitNetwork, Type: "network", Message: msg, Recover: true}
}

// NewInternalError creates an error for unexpected failures.
func NewInternalError(msg string) *CLIError {
	return &CLIError{Code: ExitInternal, Type: "internal", Message: msg}
}

// ExitCode extracts the exit code from an error.
// Returns ExitInternal (1) for non-CLIError errors, ExitOK (0) for nil.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var ce *CLIError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ExitInternal
}

// FormatError writes the error to w. In JSON mode, it writes structured JSON.
// In text mode, it writes "error: <message>".
func FormatError(w io.Writer, err error, jsonMode bool) {
	if err == nil {
		return
	}

	if jsonMode {
		var ce *CLIError
		if !errors.As(err, &ce) {
			ce = &CLIError{
				Code:    ExitInternal,
				Type:    "internal",
				Message: err.Error(),
			}
		}
		data, _ := json.Marshal(ce)
		_, _ = fmt.Fprintln(w, string(data))
		return
	}

	_, _ = fmt.Fprintf(w, "error: %v\n", err)
}
