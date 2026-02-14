package main

import (
	"fmt"
	"regexp"

	"github.com/ppiankov/logtap/internal/archive"
	"github.com/ppiankov/logtap/internal/recv"
)

// buildFilter constructs an archive.Filter from CLI flags.
// Returns nil if no filter flags are set.
func buildFilter(fromStr, toStr string, labels []string, grepStr string, meta *recv.Metadata) (*archive.Filter, error) {
	hasFilter := fromStr != "" || toStr != "" || len(labels) > 0 || grepStr != ""
	if !hasFilter {
		return nil, nil
	}

	f := &archive.Filter{}

	refDate := meta.Started
	refTime := meta.Stopped
	if refTime.IsZero() {
		refTime = meta.Started
	}

	if fromStr != "" {
		t, err := archive.ParseTimeFlag(fromStr, refDate, refTime)
		if err != nil {
			return nil, fmt.Errorf("invalid --from: %w", err)
		}
		f.From = t
	}
	if toStr != "" {
		t, err := archive.ParseTimeFlag(toStr, refDate, refTime)
		if err != nil {
			return nil, fmt.Errorf("invalid --to: %w", err)
		}
		f.To = t
	}

	for _, l := range labels {
		lm, err := archive.ParseLabelFlag(l)
		if err != nil {
			return nil, fmt.Errorf("invalid --label: %w", err)
		}
		f.Labels = append(f.Labels, lm)
	}

	if grepStr != "" {
		re, err := regexp.Compile(grepStr)
		if err != nil {
			return nil, fmt.Errorf("invalid --grep: %w", err)
		}
		f.Grep = re
	}

	return f, nil
}
