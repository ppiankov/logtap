package archive

import (
	"fmt"
	"strings"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

// FaultType identifies the kind of fault to inject.
type FaultType string

const (
	FaultErrorSpike   FaultType = "error-spike"
	FaultServiceDown  FaultType = "service-down"
	FaultLatencySpike FaultType = "latency-spike"
)

// FaultConfig describes a fault injection.
type FaultConfig struct {
	Type     FaultType
	Service  string        // target service (for service-down, latency-spike)
	At       time.Time     // when to start injection
	Duration time.Duration // how long the fault lasts
}

// ParseFault parses a fault spec string.
// Formats: "error-spike", "service-down=payment", "latency-spike=api".
func ParseFault(spec string) (FaultConfig, error) {
	parts := strings.SplitN(spec, "=", 2)
	ft := FaultType(parts[0])

	switch ft {
	case FaultErrorSpike:
		if len(parts) > 1 {
			return FaultConfig{}, fmt.Errorf("error-spike does not accept a service parameter")
		}
		return FaultConfig{Type: ft}, nil
	case FaultServiceDown, FaultLatencySpike:
		if len(parts) != 2 || parts[1] == "" {
			return FaultConfig{}, fmt.Errorf("%s requires service name: %s=<service>", ft, ft)
		}
		return FaultConfig{Type: ft, Service: parts[1]}, nil
	default:
		return FaultConfig{}, fmt.Errorf("unknown fault type %q (valid: error-spike, service-down=<svc>, latency-spike=<svc>)", parts[0])
	}
}

var syntheticErrors = []string{
	"error: connection timeout",
	"error: 503 service unavailable",
	"panic: runtime error",
}

// NewInjector creates a transform function that applies fault injection.
// Entries outside the fault window pass through unchanged.
func NewInjector(faults []FaultConfig) func(recv.LogEntry) []recv.LogEntry {
	return func(e recv.LogEntry) []recv.LogEntry {
		result := []recv.LogEntry{e}

		for _, f := range faults {
			end := f.At.Add(f.Duration)
			inWindow := !e.Timestamp.Before(f.At) && e.Timestamp.Before(end)

			switch f.Type {
			case FaultErrorSpike:
				if inWindow {
					for _, msg := range syntheticErrors {
						result = append(result, recv.LogEntry{
							Timestamp: e.Timestamp,
							Labels:    withInjected(e.Labels),
							Message:   msg,
						})
					}
				}

			case FaultServiceDown:
				if inWindow {
					svc := e.Labels["app"]
					if svc == f.Service {
						// drop entries from the downed service
						result = filterOut(result, f.Service)
					}
				}

			case FaultLatencySpike:
				if inWindow {
					svc := e.Labels["app"]
					if svc == f.Service {
						for i := range result {
							if result[i].Labels["app"] == f.Service {
								result[i].Message += " [SLOW: 5.2s]"
								result[i].Labels = withInjected(result[i].Labels)
							}
						}
					}
				}
			}
		}

		return result
	}
}

// withInjected returns a copy of labels with _injected=true added.
func withInjected(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		out[k] = v
	}
	out["_injected"] = "true"
	return out
}

// filterOut removes entries whose app label matches the service.
func filterOut(entries []recv.LogEntry, service string) []recv.LogEntry {
	var kept []recv.LogEntry
	for _, e := range entries {
		if e.Labels["app"] != service {
			kept = append(kept, e)
		}
	}
	return kept
}
