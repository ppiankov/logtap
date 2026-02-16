package archive

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

func TestWriteHTML_Basic(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	result := &TriageResult{
		Dir: "/tmp/capture-001",
		Meta: &recv.Metadata{
			Started: base,
			Stopped: base.Add(9 * time.Minute),
		},
		TotalLines: 10000,
		ErrorLines: 500,
		Timeline: []TriageBucket{
			{Time: base, TotalLines: 1000, ErrorLines: 50},
			{Time: base.Add(1 * time.Minute), TotalLines: 1200, ErrorLines: 60},
			{Time: base.Add(2 * time.Minute), TotalLines: 1500, ErrorLines: 200},
			{Time: base.Add(3 * time.Minute), TotalLines: 1100, ErrorLines: 40},
			{Time: base.Add(4 * time.Minute), TotalLines: 900, ErrorLines: 30},
		},
		Errors: []ErrorSignature{
			{Signature: "connection refused to <IP>:<N>", Count: 200, FirstSeen: base.Add(2 * time.Minute), Example: "connection refused to 10.0.0.1:5432"},
			{Signature: "context deadline exceeded", Count: 150, FirstSeen: base.Add(3 * time.Minute), Example: "context deadline exceeded"},
			{Signature: "OOMKilled container <UUID>", Count: 100, FirstSeen: base.Add(1 * time.Minute), Example: "OOMKilled container abc-123"},
		},
		Talkers: map[string][]TalkerEntry{
			"app": {
				{Value: "api-gateway", TotalLines: 5000, ErrorLines: 300},
				{Value: "worker", TotalLines: 3000, ErrorLines: 150},
				{Value: "scheduler", TotalLines: 2000, ErrorLines: 50},
			},
		},
		Windows: TriageWindows{
			PeakError: &TimeWindow{
				From: base.Add(2 * time.Minute).Format(time.RFC3339),
				To:   base.Add(7 * time.Minute).Format(time.RFC3339),
				Desc: "200 errors in 5 minutes",
			},
			IncidentStart: &TimeWindow{
				From: base.Add(2 * time.Minute).Format(time.RFC3339),
				To:   base.Add(2 * time.Minute).Format(time.RFC3339),
				Desc: "3 new error signatures",
			},
		},
	}

	var buf bytes.Buffer
	if err := result.WriteHTML(&buf); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}

	html := buf.String()

	checks := []string{
		"<!DOCTYPE html>",
		"<svg",
		"capture-001",
		"10,000 lines",
		"500 errors",
		"connection refused",
		"context deadline exceeded",
		"api-gateway",
		"worker",
		"Incident Signal",
		"Peak error window",
		"logtap slice",
	}
	for _, want := range checks {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestWriteHTML_Empty(t *testing.T) {
	result := &TriageResult{
		Dir:        "/tmp/empty",
		TotalLines: 0,
		ErrorLines: 0,
	}

	var buf bytes.Buffer
	if err := result.WriteHTML(&buf); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("missing DOCTYPE")
	}
	if !strings.Contains(html, "No errors found") {
		t.Error("should contain 'No errors found' for empty result")
	}
	if strings.Contains(html, "<svg") {
		t.Error("should not contain SVG chart for empty timeline")
	}
}

func TestWriteHTML_NoWindows(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	result := &TriageResult{
		Dir:        "/tmp/no-windows",
		TotalLines: 1000,
		ErrorLines: 100,
		Timeline: []TriageBucket{
			{Time: base, TotalLines: 500, ErrorLines: 50},
			{Time: base.Add(time.Minute), TotalLines: 500, ErrorLines: 50},
		},
		Errors: []ErrorSignature{
			{Signature: "test error", Count: 100, FirstSeen: base},
		},
	}

	var buf bytes.Buffer
	if err := result.WriteHTML(&buf); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}

	html := buf.String()
	if strings.Contains(html, "Incident Signal") {
		t.Error("should not contain 'Incident Signal' when no windows")
	}
	if !strings.Contains(html, "test error") {
		t.Error("should contain error signature")
	}
}

func TestWriteHTML_LargeTimeline(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	var timeline []TriageBucket
	for i := 0; i < 60; i++ {
		timeline = append(timeline, TriageBucket{
			Time:       base.Add(time.Duration(i) * time.Minute),
			TotalLines: int64(1000 + i*10),
			ErrorLines: int64(50 + i*2),
		})
	}

	result := &TriageResult{
		Dir:        "/tmp/large",
		TotalLines: 60000,
		ErrorLines: 3000,
		Timeline:   timeline,
	}

	var buf bytes.Buffer
	if err := result.WriteHTML(&buf); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "<svg") {
		t.Error("should contain SVG for large timeline")
	}
	if !strings.Contains(html, "polyline") {
		t.Error("should contain polyline elements")
	}
}
