package archive

import (
	"regexp"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func TestParseTimeFlag(t *testing.T) {
	refDate := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	refTime := time.Date(2024, 1, 15, 10, 45, 0, 0, time.UTC)

	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:  "RFC3339",
			input: "2024-01-15T10:32:00Z",
			want:  time.Date(2024, 1, 15, 10, 32, 0, 0, time.UTC),
		},
		{
			name:  "HH:MM",
			input: "10:32",
			want:  time.Date(2024, 1, 15, 10, 32, 0, 0, time.UTC),
		},
		{
			name:  "relative 30m",
			input: "-30m",
			want:  time.Date(2024, 1, 15, 10, 15, 0, 0, time.UTC),
		},
		{
			name:  "relative 2h",
			input: "-2h",
			want:  time.Date(2024, 1, 15, 8, 45, 0, 0, time.UTC),
		},
		{
			name:  "empty string",
			input: "",
			want:  time.Time{},
		},
		{
			name:    "invalid",
			input:   "not-a-time",
			wantErr: true,
		},
		{
			name:    "invalid relative",
			input:   "-xyz",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTimeFlag(tt.input, refDate, refTime)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("ParseTimeFlag(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseLabelFlag(t *testing.T) {
	tests := []struct {
		input   string
		wantKey string
		wantVal string
		wantErr bool
	}{
		{"app=api", "app", "api", false},
		{"env=prod", "env", "prod", false},
		{"key=val=ue", "key", "val=ue", false}, // value can contain =
		{"", "", "", true},
		{"noequals", "", "", true},
		{"=value", "", "", true}, // empty key
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLabelFlag(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Key != tt.wantKey || got.Value != tt.wantVal {
				t.Errorf("ParseLabelFlag(%q) = {%q, %q}, want {%q, %q}",
					tt.input, got.Key, got.Value, tt.wantKey, tt.wantVal)
			}
		})
	}
}

func TestSkipFileTime(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	idx := &rotate.IndexEntry{
		From: base,
		To:   base.Add(10 * time.Minute),
	}

	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want bool
	}{
		{"no filter", time.Time{}, time.Time{}, false},
		{"overlaps", base.Add(5 * time.Minute), base.Add(15 * time.Minute), false},
		{"contains", base.Add(-1 * time.Minute), base.Add(15 * time.Minute), false},
		{"after file", base.Add(20 * time.Minute), time.Time{}, true},
		{"before file", time.Time{}, base.Add(-1 * time.Minute), true},
		{"exact match", base, base.Add(10 * time.Minute), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Filter{From: tt.from, To: tt.to}
			got := f.SkipFile(idx)
			if got != tt.want {
				t.Errorf("SkipFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSkipFileLabels(t *testing.T) {
	idx := &rotate.IndexEntry{
		Labels: map[string]map[string]int64{
			"app": {"api": 100, "web": 50},
			"env": {"prod": 150},
		},
	}

	tests := []struct {
		name   string
		labels []LabelMatcher
		want   bool
	}{
		{"matching label", []LabelMatcher{{Key: "app", Value: "api"}}, false},
		{"missing value", []LabelMatcher{{Key: "app", Value: "worker"}}, true},
		{"unknown key", []LabelMatcher{{Key: "region", Value: "us"}}, false}, // key not in index, cannot skip
		{"multiple matching", []LabelMatcher{{Key: "app", Value: "api"}, {Key: "env", Value: "prod"}}, false},
		{"one mismatch", []LabelMatcher{{Key: "app", Value: "api"}, {Key: "env", Value: "staging"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Filter{Labels: tt.labels}
			got := f.SkipFile(idx)
			if got != tt.want {
				t.Errorf("SkipFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSkipFileNilIndex(t *testing.T) {
	f := &Filter{From: time.Now()}
	if f.SkipFile(nil) {
		t.Error("should not skip nil index (orphan file)")
	}
}

func TestSkipFileNilFilter(t *testing.T) {
	var f *Filter
	idx := &rotate.IndexEntry{}
	if f.SkipFile(idx) {
		t.Error("nil filter should not skip")
	}
}

func TestMatchEntryTime(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	entry := recv.LogEntry{
		Timestamp: base.Add(5 * time.Minute),
		Labels:    map[string]string{"app": "api"},
		Message:   "hello",
	}

	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want bool
	}{
		{"no filter", time.Time{}, time.Time{}, true},
		{"in range", base, base.Add(10 * time.Minute), true},
		{"before range", base.Add(6 * time.Minute), time.Time{}, false},
		{"after range", time.Time{}, base.Add(4 * time.Minute), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Filter{From: tt.from, To: tt.to}
			got := f.MatchEntry(entry)
			if got != tt.want {
				t.Errorf("MatchEntry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchEntryLabels(t *testing.T) {
	entry := recv.LogEntry{
		Timestamp: time.Now(),
		Labels:    map[string]string{"app": "api", "env": "prod"},
		Message:   "hello",
	}

	tests := []struct {
		name   string
		labels []LabelMatcher
		want   bool
	}{
		{"matching", []LabelMatcher{{Key: "app", Value: "api"}}, true},
		{"not matching", []LabelMatcher{{Key: "app", Value: "web"}}, false},
		{"multiple matching", []LabelMatcher{{Key: "app", Value: "api"}, {Key: "env", Value: "prod"}}, true},
		{"one mismatch", []LabelMatcher{{Key: "app", Value: "api"}, {Key: "env", Value: "staging"}}, false},
		{"missing key", []LabelMatcher{{Key: "region", Value: "us"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Filter{Labels: tt.labels}
			got := f.MatchEntry(entry)
			if got != tt.want {
				t.Errorf("MatchEntry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchEntryGrep(t *testing.T) {
	entry := recv.LogEntry{
		Timestamp: time.Now(),
		Labels:    map[string]string{"app": "api"},
		Message:   "GET /health 200 OK",
	}

	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{"matching", "health", true},
		{"not matching", "error", false},
		{"regex", `GET.*200`, true},
		{"regex no match", `POST.*500`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Filter{Grep: regexp.MustCompile(tt.pattern)}
			got := f.MatchEntry(entry)
			if got != tt.want {
				t.Errorf("MatchEntry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchEntryCombined(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	entry := recv.LogEntry{
		Timestamp: base.Add(5 * time.Minute),
		Labels:    map[string]string{"app": "api"},
		Message:   "GET /health 200",
	}

	f := &Filter{
		From:   base,
		To:     base.Add(10 * time.Minute),
		Labels: []LabelMatcher{{Key: "app", Value: "api"}},
		Grep:   regexp.MustCompile("health"),
	}
	if !f.MatchEntry(entry) {
		t.Error("expected match with all criteria satisfied")
	}

	// fail on label
	f2 := &Filter{
		From:   base,
		To:     base.Add(10 * time.Minute),
		Labels: []LabelMatcher{{Key: "app", Value: "web"}},
		Grep:   regexp.MustCompile("health"),
	}
	if f2.MatchEntry(entry) {
		t.Error("expected no match with wrong label")
	}

	// fail on grep
	f3 := &Filter{
		From:   base,
		To:     base.Add(10 * time.Minute),
		Labels: []LabelMatcher{{Key: "app", Value: "api"}},
		Grep:   regexp.MustCompile("error"),
	}
	if f3.MatchEntry(entry) {
		t.Error("expected no match with wrong grep")
	}
}

func TestMatchEntryNilFilter(t *testing.T) {
	var f *Filter
	entry := recv.LogEntry{Message: "anything"}
	if !f.MatchEntry(entry) {
		t.Error("nil filter should match everything")
	}
}
