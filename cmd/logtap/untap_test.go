package main

import "testing"

func TestRunUntap_Validation(t *testing.T) {
	tests := []struct {
		name    string
		opts    untapOpts
		wantErr string
	}{
		{
			name:    "no session or all",
			opts:    untapOpts{},
			wantErr: "specify --session or --all",
		},
		{
			name:    "session and all",
			opts:    untapOpts{session: "lt-1234", all: true},
			wantErr: "mutually exclusive",
		},
		{
			name:    "all without force",
			opts:    untapOpts{all: true},
			wantErr: "requires --force",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runUntap(tt.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !containsString(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
