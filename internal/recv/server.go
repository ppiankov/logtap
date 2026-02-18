package recv

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// LokiPushRequest is the Loki push API JSON payload.
type LokiPushRequest struct {
	Streams []LokiStream `json:"streams"`
}

// LokiStream is one stream within a Loki push request.
type LokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"` // [ns_timestamp, message]
}

const maxRequestBytes = 10 << 20 // 10MB

// APIVersion is incremented on breaking changes to the push API.
const APIVersion = 1

// Server is the HTTP receiver server.
type Server struct {
	httpSrv    *http.Server
	writer     *Writer
	redactor   *Redactor
	metrics    *Metrics
	stats      *Stats
	ring       *LogRing
	audit      *AuditLogger
	activeConn atomic.Int64
	version    string
}

// NewServer creates an HTTP server bound to addr.
func NewServer(addr string, writer *Writer, redactor *Redactor, metrics *Metrics, stats *Stats, ring *LogRing) *Server {
	s := &Server{
		writer:   writer,
		redactor: redactor,
		metrics:  metrics,
		stats:    stats,
		ring:     ring,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /loki/api/v1/push", s.handleLokiPush)
	mux.HandleFunc("POST /logtap/raw", s.handleRawPush)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /api/version", s.handleVersion)
	mux.Handle("GET /metrics", promhttp.Handler())

	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return s
}

// SetAuditLogger attaches an audit logger to the server.
func (s *Server) SetAuditLogger(a *AuditLogger) {
	s.audit = a
}

// SetVersion sets the application version reported by /api/version.
func (s *Server) SetVersion(v string) {
	s.version = v
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	return s.httpSrv.ListenAndServe()
}

// ListenAndServeTLS starts the HTTP server with TLS.
func (s *Server) ListenAndServeTLS(certFile, keyFile string) error {
	return s.httpSrv.ListenAndServeTLS(certFile, keyFile)
}

// Serve accepts connections on a listener.
func (s *Server) Serve(ln net.Listener) error {
	return s.httpSrv.Serve(ln)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) handleLokiPush(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.trackConnOpen()
	defer s.trackConnClose()
	defer func() {
		if s.metrics != nil {
			s.metrics.PushDuration.Observe(time.Since(start).Seconds())
		}
	}()

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)

	var req LokiPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	var lineCount int
	var byteCount int
	for _, stream := range req.Streams {
		for _, val := range stream.Values {
			if len(val) < 2 {
				continue
			}
			ts := parseNanoTimestamp(val[0])
			msg := val[1]

			if s.redactor != nil {
				msg = s.redactor.Redact(msg)
			}

			lineCount++
			byteCount += len(msg)

			entry := LogEntry{
				Timestamp: ts,
				Labels:    stream.Stream,
				Message:   msg,
			}

			if s.ring != nil {
				s.ring.Push(entry)
			}

			if s.writer.Send(entry) {
				if s.metrics != nil {
					s.metrics.LogsReceived.Inc()
				}
				if s.stats != nil {
					s.stats.RecordEntry(stream.Stream)
				}
			} else {
				if s.metrics != nil {
					s.metrics.LogsDropped.Inc()
					s.metrics.BackpressureEvents.Inc()
				}
				if s.stats != nil {
					s.stats.RecordDrop()
				}
			}
		}
	}

	s.audit.Log(AuditEntry{
		Event:    "loki_push_received",
		RemoteIP: stripPort(r.RemoteAddr),
		Lines:    lineCount,
		Bytes:    byteCount,
		Duration: time.Since(start),
	})

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRawPush(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.trackConnOpen()
	defer s.trackConnClose()
	defer func() {
		if s.metrics != nil {
			s.metrics.PushDuration.Observe(time.Since(start).Seconds())
		}
	}()

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)

	var lines []LogEntry
	dec := json.NewDecoder(r.Body)
	for dec.More() {
		var entry LogEntry
		if err := dec.Decode(&entry); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON line: %v", err), http.StatusBadRequest)
			return
		}
		if s.redactor != nil {
			entry.Message = s.redactor.Redact(entry.Message)
		}
		lines = append(lines, entry)
	}

	var lineCount int
	var byteCount int
	for _, entry := range lines {
		if entry.Timestamp.IsZero() {
			entry.Timestamp = time.Now()
		}

		lineCount++
		byteCount += len(entry.Message)

		if s.ring != nil {
			s.ring.Push(entry)
		}

		if s.writer.Send(entry) {
			if s.metrics != nil {
				s.metrics.LogsReceived.Inc()
			}
			if s.stats != nil {
				s.stats.RecordEntry(entry.Labels)
			}
		} else {
			if s.metrics != nil {
				s.metrics.LogsDropped.Inc()
				s.metrics.BackpressureEvents.Inc()
			}
			if s.stats != nil {
				s.stats.RecordDrop()
			}
		}
	}

	s.audit.Log(AuditEntry{
		Event:    "raw_push_received",
		RemoteIP: stripPort(r.RemoteAddr),
		Lines:    lineCount,
		Bytes:    byteCount,
		Duration: time.Since(start),
	})

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.writer != nil && !s.writer.Healthy() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not_ready","reason":"writer backpressure"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	v := s.version
	if v == "" {
		v = "dev"
	}
	resp := struct {
		Version string `json:"version"`
		API     int    `json:"api"`
	}{
		Version: v,
		API:     APIVersion,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) trackConnOpen() {
	n := s.activeConn.Add(1)
	if s.metrics != nil {
		s.metrics.ActiveConnections.Set(float64(n))
	}
	if s.stats != nil {
		s.stats.ActiveConns.Store(n)
	}
}

func (s *Server) trackConnClose() {
	n := s.activeConn.Add(-1)
	if s.metrics != nil {
		s.metrics.ActiveConnections.Set(float64(n))
	}
	if s.stats != nil {
		s.stats.ActiveConns.Store(n)
	}
}

func stripPort(addr string) string {
	if host, _, ok := strings.Cut(addr, ":"); ok {
		return host
	}
	return addr
}

func parseNanoTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	ns, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Now()
	}
	return time.Unix(0, ns)
}
