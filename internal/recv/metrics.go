package recv

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds all Prometheus metrics for the receiver pipeline.
type Metrics struct {
	LogsReceived       prometheus.Counter
	LogsDropped        prometheus.Counter
	BytesWritten       prometheus.Counter
	DiskUsage          prometheus.Gauge
	ActiveConnections  prometheus.Gauge
	BackpressureEvents prometheus.Counter
	RedactionsTotal    *prometheus.CounterVec
}

// NewMetrics creates and registers all receiver metrics.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		LogsReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "logtap_logs_received_total",
			Help: "Total log entries received",
		}),
		LogsDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "logtap_logs_dropped_total",
			Help: "Total log entries dropped due to backpressure",
		}),
		BytesWritten: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "logtap_bytes_written_total",
			Help: "Total bytes written to disk",
		}),
		DiskUsage: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "logtap_disk_usage_bytes",
			Help: "Current disk usage in bytes",
		}),
		ActiveConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "logtap_active_connections",
			Help: "Current active HTTP connections",
		}),
		BackpressureEvents: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "logtap_backpressure_events_total",
			Help: "Total backpressure events (channel full)",
		}),
		RedactionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "logtap_redactions_total",
			Help: "Total redactions applied by pattern",
		}, []string{"pattern"}),
	}
	reg.MustRegister(
		m.LogsReceived,
		m.LogsDropped,
		m.BytesWritten,
		m.DiskUsage,
		m.ActiveConnections,
		m.BackpressureEvents,
		m.RedactionsTotal,
	)
	return m
}
