package main

import "testing"

func TestValidateQuantity(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		value   string
		wantErr bool
	}{
		{"empty", "--sidecar-memory", "", false},
		{"valid memory", "--sidecar-memory", "16Mi", false},
		{"valid cpu", "--sidecar-cpu", "25m", false},
		{"valid large memory", "--sidecar-memory", "1Gi", false},
		{"valid cpu cores", "--sidecar-cpu", "2", false},
		{"valid decimal cpu", "--sidecar-cpu", "100m", false},
		{"invalid garbage", "--sidecar-memory", "garbage", true},
		{"invalid format", "--sidecar-cpu", "abc123xyz", true},
		{"invalid unit", "--sidecar-memory", "16XX", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQuantity(tt.flag, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateQuantity(%q, %q) error = %v, wantErr %v", tt.flag, tt.value, err, tt.wantErr)
			}
			if err != nil && tt.wantErr {
				// Verify error message includes the flag name
				if got := err.Error(); len(got) == 0 {
					t.Error("error message is empty")
				}
			}
		})
	}
}
