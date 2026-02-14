package archive

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
	"github.com/ppiankov/logtap/internal/rotate"
)

// LabelMatcher matches a specific label key=value pair.
type LabelMatcher struct {
	Key   string
	Value string
}

// Filter provides two-tier filtering: file-level skip and entry-level match.
type Filter struct {
	From   time.Time
	To     time.Time
	Labels []LabelMatcher
	Grep   *regexp.Regexp
}

// SkipFile returns true if the entire file can be skipped based on index metadata.
func (f *Filter) SkipFile(idx *rotate.IndexEntry) bool {
	if f == nil || idx == nil {
		return false
	}

	// time: skip if no overlap
	if !f.From.IsZero() && idx.To.Before(f.From) {
		return true
	}
	if !f.To.IsZero() && idx.From.After(f.To) {
		return true
	}

	// labels: skip if key is present in index but value is absent
	for _, lm := range f.Labels {
		if vals, ok := idx.Labels[lm.Key]; ok {
			if _, hasVal := vals[lm.Value]; !hasVal {
				return true
			}
		}
	}

	// grep: cannot skip at file level
	return false
}

// MatchEntry returns true if the entry passes all filter criteria.
func (f *Filter) MatchEntry(e recv.LogEntry) bool {
	if f == nil {
		return true
	}

	// time range
	if !f.From.IsZero() && e.Timestamp.Before(f.From) {
		return false
	}
	if !f.To.IsZero() && e.Timestamp.After(f.To) {
		return false
	}

	// labels (AND logic)
	for _, lm := range f.Labels {
		if e.Labels[lm.Key] != lm.Value {
			return false
		}
	}

	// grep
	if f.Grep != nil && !f.Grep.MatchString(e.Message) {
		return false
	}

	return true
}

// ParseTimeFlag parses a time string in one of three formats:
// - RFC3339: "2024-01-15T10:32:00Z"
// - HH:MM: "10:32" — resolved against refDate
// - Relative: "-30m" — resolved against refTime
func ParseTimeFlag(s string, refDate, refTime time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}

	// try RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// try HH:MM
	if len(s) == 5 && s[2] == ':' {
		t, err := time.Parse("15:04", s)
		if err == nil {
			return time.Date(
				refDate.Year(), refDate.Month(), refDate.Day(),
				t.Hour(), t.Minute(), 0, 0, refDate.Location(),
			), nil
		}
	}

	// try relative duration (e.g. "-30m", "-2h")
	if strings.HasPrefix(s, "-") {
		d, err := time.ParseDuration(s[1:])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		return refTime.Add(-d), nil
	}

	return time.Time{}, fmt.Errorf("unrecognized time format: %q", s)
}

// ParseLabelFlag parses a "key=value" label matcher.
func ParseLabelFlag(s string) (LabelMatcher, error) {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 || parts[0] == "" {
		return LabelMatcher{}, fmt.Errorf("invalid label filter %q: expected key=value", s)
	}
	return LabelMatcher{Key: parts[0], Value: parts[1]}, nil
}
