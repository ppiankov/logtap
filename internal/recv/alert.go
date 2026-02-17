package recv

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// AlertRule defines a threshold-based alert that fires a webhook.
type AlertRule struct {
	Name      string  `yaml:"name"`
	Metric    string  `yaml:"metric"` // logs_dropped, drop_rate, disk_pct
	Op        string  `yaml:"op"`     // gt, lt, gte, lte
	Threshold float64 `yaml:"threshold"`
	Detail    string  `yaml:"detail"`
}

// AlertRulesFile is the YAML structure for alert rules.
type AlertRulesFile struct {
	Rules []AlertRule `yaml:"rules"`
}

// AlertEngine evaluates alert rules against pipeline snapshots and fires
// webhook events when thresholds are crossed.
type AlertEngine struct {
	rules      []AlertRule
	dispatcher *WebhookDispatcher
	lastSnap   *Snapshot
	fired      map[string]bool // per-rule dedup (hysteresis)
}

// NewAlertEngine creates an engine with the given rules and webhook dispatcher.
func NewAlertEngine(rules []AlertRule, dispatcher *WebhookDispatcher) *AlertEngine {
	return &AlertEngine{
		rules:      rules,
		dispatcher: dispatcher,
		fired:      make(map[string]bool),
	}
}

// LoadAlertRules loads alert rules from a YAML file.
func LoadAlertRules(path string) ([]AlertRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read alert rules: %w", err)
	}
	var f AlertRulesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse alert rules: %w", err)
	}
	for _, r := range f.Rules {
		if err := validateRule(r); err != nil {
			return nil, err
		}
	}
	return f.Rules, nil
}

func validateRule(r AlertRule) error {
	switch r.Metric {
	case "logs_dropped", "drop_rate", "disk_pct", "logs_received":
	default:
		return fmt.Errorf("unknown alert metric: %s", r.Metric)
	}
	switch r.Op {
	case "gt", "lt", "gte", "lte":
	default:
		return fmt.Errorf("unknown alert operator: %s", r.Op)
	}
	if r.Name == "" {
		return fmt.Errorf("alert rule missing name")
	}
	return nil
}

// Evaluate checks all rules against the current snapshot. Call this every tick
// (e.g. 1s). It computes derived metrics from the delta between current and
// previous snapshots, then fires webhook events for rules that cross their
// thresholds (with hysteresis — once fired, a rule won't re-fire until the
// condition resolves).
func (e *AlertEngine) Evaluate(snap Snapshot) {
	var dropRate float64
	if e.lastSnap != nil {
		dropRate = float64(snap.LogsDropped - e.lastSnap.LogsDropped)
		if dropRate < 0 {
			dropRate = 0
		}
	}

	var diskPct float64
	if snap.DiskCap > 0 {
		diskPct = float64(snap.DiskUsage) * 100 / float64(snap.DiskCap)
	}

	for _, rule := range e.rules {
		var val float64
		switch rule.Metric {
		case "logs_dropped":
			val = float64(snap.LogsDropped)
		case "drop_rate":
			val = dropRate
		case "disk_pct":
			val = diskPct
		case "logs_received":
			val = float64(snap.LogsReceived)
		}

		triggered := compare(val, rule.Op, rule.Threshold)
		if triggered && !e.fired[rule.Name] {
			e.fired[rule.Name] = true
			e.dispatcher.Fire(WebhookEvent{
				Event:     "alert",
				Timestamp: time.Now(),
				Detail:    rule.Detail,
			})
		} else if !triggered && e.fired[rule.Name] {
			e.fired[rule.Name] = false
		}
	}

	snapCopy := snap
	e.lastSnap = &snapCopy
}

// Fired returns the names of rules currently in the fired state.
func (e *AlertEngine) Fired() []string {
	var names []string
	for name, f := range e.fired {
		if f {
			names = append(names, name)
		}
	}
	return names
}

func compare(val float64, op string, threshold float64) bool {
	switch op {
	case "gt":
		return val > threshold
	case "lt":
		return val < threshold
	case "gte":
		return val >= threshold
	case "lte":
		return val <= threshold
	}
	return false
}
