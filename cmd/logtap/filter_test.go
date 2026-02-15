package main

import (
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

func TestBuildFilter_NoFlags(t *testing.T) {
	meta := &recv.Metadata{Started: time.Now()}
	f, err := buildFilter("", "", nil, "", meta)
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Error("expected nil filter when no flags set")
	}
}

func TestBuildFilter_FromTo(t *testing.T) {
	started := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	stopped := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	meta := &recv.Metadata{Started: started, Stopped: stopped}

	f, err := buildFilter("10:30", "11:30", nil, "", meta)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatal("expected non-nil filter")
	}
	if f.From.Hour() != 10 || f.From.Minute() != 30 {
		t.Errorf("expected from 10:30, got %v", f.From)
	}
	if f.To.Hour() != 11 || f.To.Minute() != 30 {
		t.Errorf("expected to 11:30, got %v", f.To)
	}
}

func TestBuildFilter_InvalidFrom(t *testing.T) {
	meta := &recv.Metadata{Started: time.Now()}
	_, err := buildFilter("not-a-time", "", nil, "", meta)
	if err == nil {
		t.Error("expected error for invalid --from")
	}
}

func TestBuildFilter_InvalidTo(t *testing.T) {
	meta := &recv.Metadata{Started: time.Now()}
	_, err := buildFilter("", "not-a-time", nil, "", meta)
	if err == nil {
		t.Error("expected error for invalid --to")
	}
}

func TestBuildFilter_Labels(t *testing.T) {
	meta := &recv.Metadata{Started: time.Now()}
	f, err := buildFilter("", "", []string{"app=web", "env=staging"}, "", meta)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(f.Labels))
	}
	if f.Labels[0].Key != "app" || f.Labels[0].Value != "web" {
		t.Errorf("label 0: got %s=%s", f.Labels[0].Key, f.Labels[0].Value)
	}
}

func TestBuildFilter_InvalidLabel(t *testing.T) {
	meta := &recv.Metadata{Started: time.Now()}
	_, err := buildFilter("", "", []string{"noequalssign"}, "", meta)
	if err == nil {
		t.Error("expected error for invalid label format")
	}
}

func TestBuildFilter_Grep(t *testing.T) {
	meta := &recv.Metadata{Started: time.Now()}
	f, err := buildFilter("", "", nil, "error|panic", meta)
	if err != nil {
		t.Fatal(err)
	}
	if f.Grep == nil {
		t.Error("expected grep regex to be set")
	}
	if !f.Grep.MatchString("some error here") {
		t.Error("grep should match 'error'")
	}
}

func TestBuildFilter_InvalidGrep(t *testing.T) {
	meta := &recv.Metadata{Started: time.Now()}
	_, err := buildFilter("", "", nil, "[invalid(", meta)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestBuildFilter_RelativeTime(t *testing.T) {
	stopped := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	meta := &recv.Metadata{Started: stopped.Add(-2 * time.Hour), Stopped: stopped}

	f, err := buildFilter("-30m", "", nil, "", meta)
	if err != nil {
		t.Fatal(err)
	}
	expected := stopped.Add(-30 * time.Minute)
	if !f.From.Equal(expected) {
		t.Errorf("expected from %v, got %v", expected, f.From)
	}
}

func TestBuildFilter_RFC3339(t *testing.T) {
	meta := &recv.Metadata{Started: time.Now()}
	f, err := buildFilter("2025-01-15T10:30:00Z", "2025-01-15T11:30:00Z", nil, "", meta)
	if err != nil {
		t.Fatal(err)
	}
	if f.From.Hour() != 10 || f.From.Minute() != 30 {
		t.Errorf("unexpected from: %v", f.From)
	}
}

func TestParseSpeed(t *testing.T) {
	tests := []struct {
		input   string
		want    float64
		wantErr bool
	}{
		{"0", 0, false},
		{"1", 1, false},
		{"10", 10, false},
		{"10x", 10, false},
		{"0.5", 0.5, false},
		{"2.5x", 2.5, false},
		{"-1", 0, true},
		{"abc", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSpeed(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSpeed(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && float64(got) != tt.want {
				t.Errorf("parseSpeed(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"100", 100, false},
		{"100B", 100, false},
		{"1KB", 1024, false},
		{"1kb", 1024, false},
		{"256MB", 256 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"1TB", 1024 * 1024 * 1024 * 1024, false},
		{"2.5GB", int64(2.5 * 1024 * 1024 * 1024), false},
		{"invalid", 0, true},
		{"MB", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseByteSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseByteSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseByteSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestDoubleResource(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"16Mi", "32Mi"},
		{"25m", "50m"},
		{"100m", "200m"},
		{"1Gi", "2Gi"},
		{"invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := doubleResource(tt.input)
			if got != tt.want {
				t.Errorf("doubleResource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseExportFormat(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"parquet", "parquet", false},
		{"csv", "csv", false},
		{"jsonl", "jsonl", false},
		{"xml", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExportFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseExportFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && string(got) != tt.want {
				t.Errorf("parseExportFormat(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{int64(2.5 * 1024 * 1024 * 1024), "2.5 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.input)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildFilter_AllFlags(t *testing.T) {
	started := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	stopped := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	meta := &recv.Metadata{Started: started, Stopped: stopped}

	f, err := buildFilter("10:30", "11:30", []string{"app=web"}, "error", meta)
	if err != nil {
		t.Fatal(err)
	}
	if f.From.IsZero() {
		t.Error("from should be set")
	}
	if f.To.IsZero() {
		t.Error("to should be set")
	}
	if len(f.Labels) != 1 {
		t.Error("labels should have 1 entry")
	}
	if f.Grep == nil {
		t.Error("grep should be set")
	}
}

func TestBuildFilter_ZeroStopped(t *testing.T) {
	// When Stopped is zero, refTime should fall back to Started
	started := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	meta := &recv.Metadata{Started: started}

	f, err := buildFilter("-30m", "", nil, "", meta)
	if err != nil {
		t.Fatal(err)
	}
	expected := started.Add(-30 * time.Minute)
	if !f.From.Equal(expected) {
		t.Errorf("expected from %v, got %v", expected, f.From)
	}
}
