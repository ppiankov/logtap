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
	PushDuration       prometheus.Histogram
	WriterQueueLength  prometheus.Gauge
	RotationTotal      *prometheus.CounterVec
	RotationErrors     prometheus.Counter
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
		PushDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "logtap_push_duration_seconds",
			Help:    "Duration of push API request handling",
			Buckets: prometheus.DefBuckets,
		}),
		WriterQueueLength: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "logtap_writer_queue_length",
			Help: "Current writer channel occupancy",
		}),
		RotationTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "logtap_rotation_total",
			Help: "Total file rotations",
		}, []string{"reason"}),
		RotationErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "logtap_rotation_errors_total",
			Help: "Total failed file rotations",
		}),
	}
	reg.MustRegister(
		m.LogsReceived,
		m.LogsDropped,
		m.BytesWritten,
		m.DiskUsage,
		m.ActiveConnections,
		m.BackpressureEvents,
		m.RedactionsTotal,
		m.PushDuration,
		m.WriterQueueLength,
		m.RotationTotal,
		m.RotationErrors,
	)
	return m
}
