package archive

import (
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

func setupCorrelateDir(t *testing.T, entries []recv.LogEntry) string {
	t.Helper()
	dir := t.TempDir()

	if len(entries) == 0 {
		base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
		writeMetadata(t, dir, base, base, 0)
		return dir
	}

	first := entries[0].Timestamp
	last := entries[len(entries)-1].Timestamp

	writeMetadata(t, dir, first, last, int64(len(entries)))
	writeDataFile(t, dir, "2024-01-15T100000-000.jsonl", entries)

	// build label counts from entries
	labels := make(map[string]map[string]int64)
	for _, e := range entries {
		for k, v := range e.Labels {
			if labels[k] == nil {
				labels[k] = make(map[string]int64)
			}
			labels[k][v]++
		}
	}

	writeIndex(t, dir, []rotate.IndexEntry{{
		File:   "2024-01-15T100000-000.jsonl",
		From:   first,
		To:     last,
		Lines:  int64(len(entries)),
		Bytes:  int64(len(entries) * 100),
		Labels: labels,
	}})

	return dir
}

func TestCorrelate_CascadeDetection(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	window := 10 * time.Second

	// Service A fails, then service B errors 10-20s later in a repeating pattern.
	// This creates a temporal cascade: A leads B by ~1 window.
	var entries []recv.LogEntry
	for i := 0; i < 10; i++ {
		offset := time.Duration(i) * 30 * time.Second
		// service A error first
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(offset),
			Labels:    map[string]string{"app": "payments"},
			Message:   "connection refused to database",
		})
		// service B error ~10s later
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(offset + 10*time.Second),
			Labels:    map[string]string{"app": "api"},
			Message:   "timeout calling payments service",
		})
	}

	dir := setupCorrelateDir(t, entries)

	correlations, err := Correlate(dir, window)
	if err != nil {
		t.Fatal(err)
	}

	if len(correlations) == 0 {
		t.Fatal("expected at least one correlation, got none")
	}

	// verify the correlation links payments → api (or vice versa)
	c := correlations[0]
	hasPair := (c.Source == "payments" && c.Target == "api") ||
		(c.Source == "api" && c.Target == "payments")
	if !hasPair {
		t.Errorf("expected correlation between payments and api, got %s → %s", c.Source, c.Target)
	}

	if c.Confidence <= minConfidence {
		t.Errorf("confidence = %.2f, want > %.2f", c.Confidence, minConfidence)
	}

	if c.LagSeconds < 0 {
		t.Errorf("lag_seconds = %.1f, want >= 0", c.LagSeconds)
	}

	if c.Pattern == "" {
		t.Error("pattern is empty")
	}

	if c.SourceError == "" || c.TargetError == "" {
		t.Error("source_error or target_error is empty")
	}
}

func TestCorrelate_NoCascade(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	window := 10 * time.Second

	// Two services error at completely different, non-overlapping times.
	// No temporal correlation should be detected.
	var entries []recv.LogEntry

	// Service A: errors only in the first 30 seconds
	for i := 0; i < 5; i++ {
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(time.Duration(i) * 5 * time.Second),
			Labels:    map[string]string{"app": "auth"},
			Message:   "error connecting to ldap",
		})
	}
	// Service B: errors 5 minutes later (no overlap)
	for i := 0; i < 5; i++ {
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(5*time.Minute + time.Duration(i)*5*time.Second),
			Labels:    map[string]string{"app": "billing"},
			Message:   "error processing payment",
		})
	}

	dir := setupCorrelateDir(t, entries)

	correlations, err := Correlate(dir, window)
	if err != nil {
		t.Fatal(err)
	}

	// should find no high-confidence correlations
	for _, c := range correlations {
		if c.Confidence > minConfidence {
			t.Errorf("unexpected correlation: %s → %s (confidence=%.2f, pattern=%s)",
				c.Source, c.Target, c.Confidence, c.Pattern)
		}
	}
}

func TestCorrelate_CoFailure(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	window := 10 * time.Second

	// Both services fail in the same windows simultaneously.
	var entries []recv.LogEntry
	for i := 0; i < 10; i++ {
		offset := time.Duration(i) * 30 * time.Second
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(offset),
			Labels:    map[string]string{"app": "frontend"},
			Message:   "error rendering page",
		})
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(offset + 1*time.Second), // same window (within 10s)
			Labels:    map[string]string{"app": "backend"},
			Message:   "error processing request",
		})
	}

	dir := setupCorrelateDir(t, entries)

	correlations, err := Correlate(dir, window)
	if err != nil {
		t.Fatal(err)
	}

	if len(correlations) == 0 {
		t.Fatal("expected at least one correlation for co-failure")
	}

	c := correlations[0]
	if c.Pattern != "co_failure" {
		t.Errorf("pattern = %q, want %q", c.Pattern, "co_failure")
	}

	if c.LagSeconds >= window.Seconds() {
		t.Errorf("lag_seconds = %.1f, want < %.1f for co_failure", c.LagSeconds, window.Seconds())
	}
}

func TestCorrelate_EmptyInput(t *testing.T) {
	dir := setupCorrelateDir(t, nil)

	correlations, err := Correlate(dir, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if len(correlations) != 0 {
		t.Errorf("expected no correlations, got %d", len(correlations))
	}
}

func TestCorrelate_SingleService(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// only one service — need at least 2 for correlation
	entries := []recv.LogEntry{
		{Timestamp: base, Labels: map[string]string{"app": "api"}, Message: "error connecting"},
		{Timestamp: base.Add(10 * time.Second), Labels: map[string]string{"app": "api"}, Message: "timeout occurred"},
	}

	dir := setupCorrelateDir(t, entries)

	correlations, err := Correlate(dir, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if len(correlations) != 0 {
		t.Errorf("expected no correlations for single service, got %d", len(correlations))
	}
}

func TestCorrelate_CascadeTimeout(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	window := 10 * time.Second

	// Service A errors mention service B name → cascade_timeout pattern.
	var entries []recv.LogEntry
	for i := 0; i < 10; i++ {
		offset := time.Duration(i) * 30 * time.Second
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(offset),
			Labels:    map[string]string{"app": "gateway"},
			Message:   "timeout calling checkout service",
		})
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(offset + 10*time.Second),
			Labels:    map[string]string{"app": "checkout"},
			Message:   "error processing order",
		})
	}

	dir := setupCorrelateDir(t, entries)

	correlations, err := Correlate(dir, window)
	if err != nil {
		t.Fatal(err)
	}

	if len(correlations) == 0 {
		t.Fatal("expected correlation for cascade_timeout")
	}

	c := correlations[0]
	if c.Pattern != "cascade_timeout" {
		t.Errorf("pattern = %q, want %q", c.Pattern, "cascade_timeout")
	}
}

func TestCorrelate_FallbackLabel(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	window := 10 * time.Second

	// No "app" label — falls back to first label value.
	var entries []recv.LogEntry
	for i := 0; i < 10; i++ {
		offset := time.Duration(i) * 30 * time.Second
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(offset),
			Labels:    map[string]string{"service": "cache"},
			Message:   "error eviction failed",
		})
		entries = append(entries, recv.LogEntry{
			Timestamp: base.Add(offset),
			Labels:    map[string]string{"service": "store"},
			Message:   "error write timeout",
		})
	}

	dir := setupCorrelateDir(t, entries)

	correlations, err := Correlate(dir, window)
	if err != nil {
		t.Fatal(err)
	}

	// should still detect correlation using fallback labels
	if len(correlations) == 0 {
		t.Fatal("expected correlation even without 'app' label")
	}

	c := correlations[0]
	hasPair := (c.Source == "cache" && c.Target == "store") ||
		(c.Source == "store" && c.Target == "cache")
	if !hasPair {
		t.Errorf("expected correlation between cache and store, got %s → %s", c.Source, c.Target)
	}
}

func TestServiceLabel(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"app label", map[string]string{"app": "api", "env": "prod"}, "api"},
		{"no app label", map[string]string{"service": "web"}, "web"},
		{"empty labels", map[string]string{}, ""},
		{"nil labels", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serviceLabel(tt.labels)
			if got != tt.want {
				t.Errorf("serviceLabel(%v) = %q, want %q", tt.labels, got, tt.want)
			}
		})
	}
}

func TestMentionsService(t *testing.T) {
	tests := []struct {
		msg  string
		svc  string
		want bool
	}{
		{"timeout calling payments service", "payments", true},
		{"error connecting to db", "payments", false},
		{"Connection to PAYMENTS failed", "payments", true}, // case insensitive
		{"", "api", false},
		{"error message", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.msg+"_"+tt.svc, func(t *testing.T) {
			got := mentionsService(tt.msg, tt.svc)
			if got != tt.want {
				t.Errorf("mentionsService(%q, %q) = %v, want %v", tt.msg, tt.svc, got, tt.want)
			}
		})
	}
}
