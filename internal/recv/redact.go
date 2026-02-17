package recv

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// RedactPattern defines a named PII pattern with its compiled regex.
type RedactPattern struct {
	Name        string `yaml:"name"`
	Pattern     string `yaml:"pattern"`
	Replacement string `yaml:"replacement"`
	re          *regexp.Regexp
	validate    func(string) bool // optional post-match validation (e.g. Luhn)
}

// Redactor holds active patterns and redacts matching content.
type Redactor struct {
	patterns []RedactPattern
	onRedact func(pattern string) // optional callback for each redaction hit
}

var builtinPatterns = []RedactPattern{
	{
		Name:        "credit_card",
		Pattern:     `\b(\d[ -]*?){13,19}\b`,
		Replacement: "[REDACTED:cc]",
	},
	{
		Name:        "email",
		Pattern:     `\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`,
		Replacement: "[REDACTED:email]",
	},
	{
		Name:        "jwt",
		Pattern:     `eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`,
		Replacement: "[REDACTED:jwt]",
	},
	{
		Name:        "bearer",
		Pattern:     `(?i)(?:Bearer\s+|Authorization:\s*Bearer\s+)[A-Za-z0-9_\-.]+`,
		Replacement: "[REDACTED:bearer]",
	},
	{
		Name:        "ip_v4",
		Pattern:     `\b(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]\d|\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]\d|\d)\b`,
		Replacement: "[REDACTED:ip]",
	},
	{
		Name:        "ssn",
		Pattern:     `\b\d{3}-\d{2}-\d{4}\b`,
		Replacement: "[REDACTED:ssn]",
	},
	{
		Name:        "phone",
		Pattern:     `(?:\+\d{1,3}[\s.-]?)?\(?\d{3}\)?[\s.-]?\d{3}[\s.-]?\d{4}\b`,
		Replacement: "[REDACTED:phone]",
	},
}

// NewRedactor creates a Redactor with the specified patterns enabled.
// If names is empty, all built-in patterns are enabled.
func NewRedactor(names []string) (*Redactor, error) {
	var selected []RedactPattern
	if len(names) == 0 {
		selected = append(selected, builtinPatterns...)
	} else {
		byName := make(map[string]RedactPattern)
		for _, p := range builtinPatterns {
			byName[p.Name] = p
		}
		for _, n := range names {
			p, ok := byName[n]
			if !ok {
				return nil, fmt.Errorf("unknown redaction pattern: %s", n)
			}
			selected = append(selected, p)
		}
	}
	return compilePatterns(selected)
}

// LoadCustomPatterns loads additional patterns from a YAML file.
func (r *Redactor) LoadCustomPatterns(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read patterns file: %w", err)
	}
	var customs []RedactPattern
	if err := yaml.Unmarshal(data, &customs); err != nil {
		return fmt.Errorf("parse patterns file: %w", err)
	}
	compiled, err := compilePatterns(customs)
	if err != nil {
		return err
	}
	r.patterns = append(r.patterns, compiled.patterns...)
	return nil
}

// SetOnRedact sets a callback invoked for each redaction hit with the pattern name.
func (r *Redactor) SetOnRedact(fn func(pattern string)) {
	r.onRedact = fn
}

// Redact replaces all matching PII in msg with redaction markers.
func (r *Redactor) Redact(msg string) string {
	for _, p := range r.patterns {
		if p.validate != nil {
			name := p.Name
			msg = p.re.ReplaceAllStringFunc(msg, func(match string) string {
				if p.validate(match) {
					if r.onRedact != nil {
						r.onRedact(name)
					}
					return p.Replacement
				}
				return match
			})
		} else {
			before := msg
			msg = p.re.ReplaceAllString(msg, p.Replacement)
			if msg != before && r.onRedact != nil {
				r.onRedact(p.Name)
			}
		}
	}
	return msg
}

// PatternNames returns the names of active patterns.
func (r *Redactor) PatternNames() []string {
	names := make([]string, len(r.patterns))
	for i, p := range r.patterns {
		names[i] = p.Name
	}
	return names
}

func compilePatterns(patterns []RedactPattern) (*Redactor, error) {
	compiled := make([]RedactPattern, len(patterns))
	for i, p := range patterns {
		re, err := regexp.Compile(p.Pattern)
		if err != nil {
			return nil, fmt.Errorf("compile pattern %s: %w", p.Name, err)
		}
		compiled[i] = p
		compiled[i].re = re
		if p.Name == "credit_card" {
			compiled[i].validate = luhnValid
		}
	}
	return &Redactor{patterns: compiled}, nil
}

// luhnValid checks if a matched string passes the Luhn algorithm.
func luhnValid(s string) bool {
	// strip spaces and dashes
	var digits []int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		} else if c != ' ' && c != '-' {
			return false
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	// Luhn algorithm
	sum := 0
	alt := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

// ParseRedactFlag parses the --redact flag value.
// "" means disabled, "true" or empty-after-flag means all patterns, "a,b" means subset.
func ParseRedactFlag(val string) (enabled bool, names []string) {
	if val == "" {
		return false, nil
	}
	if val == "true" {
		return true, nil
	}
	parts := strings.Split(val, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return true, parts
}
