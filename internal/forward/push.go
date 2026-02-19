package forward

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxBufferBytes    = 1 << 20 // 1MB
	defaultMaxRetries = 3
	defaultMaxBackoff = 30 * time.Second
	pushPath          = "/loki/api/v1/push"
)

// TimestampedLine is a single log line with its timestamp.
type TimestampedLine struct {
	Timestamp time.Time
	Line      string
}

// lokiPushRequest matches the Loki push API JSON format.
type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

// Pusher sends log lines to a logtap receiver via the Loki push API.
type Pusher struct {
	target     string
	client     *http.Client
	maxRetries int
	maxBackoff time.Duration
	onRetry    func()
}

// NewPusher creates a Pusher targeting the given receiver address.
// Targets prefixed with https:// use TLS; plain host:port defaults to http://.
func NewPusher(target string) *Pusher {
	return NewPusherWithClient(target, &http.Client{Timeout: 10 * time.Second})
}

// NewTLSPusher creates a Pusher with TLS support.
// Set skipVerify to true for self-signed certificates.
func NewTLSPusher(target string, skipVerify bool) *Pusher {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: skipVerify, //nolint:gosec // user-controlled flag for self-signed certs
			},
		},
	}
	return NewPusherWithClient(target, client)
}

// NewPusherWithClient creates a Pusher with a custom HTTP client (useful for tests).
func NewPusherWithClient(target string, client *http.Client) *Pusher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Pusher{
		target:     target,
		client:     client,
		maxRetries: defaultMaxRetries,
		maxBackoff: defaultMaxBackoff,
	}
}

// SetMaxRetries sets the maximum number of retry attempts per push.
func (p *Pusher) SetMaxRetries(n int) { p.maxRetries = n }

// SetMaxBackoff sets the maximum backoff duration between retries.
func (p *Pusher) SetMaxBackoff(d time.Duration) { p.maxBackoff = d }

// SetOnRetry sets a callback invoked on each retry attempt.
func (p *Pusher) SetOnRetry(fn func()) { p.onRetry = fn }

// Push sends a batch of log lines with the given labels to the receiver.
// Returns ErrBufferExceeded if the serialized payload exceeds 1MB.
// Retries transient errors up to 3 times with exponential backoff.
func (p *Pusher) Push(ctx context.Context, labels map[string]string, lines []TimestampedLine) error {
	if len(lines) == 0 {
		return nil
	}

	values := make([][]string, len(lines))
	for i, l := range lines {
		values[i] = []string{strconv.FormatInt(l.Timestamp.UnixNano(), 10), l.Line}
	}

	req := lokiPushRequest{
		Streams: []lokiStream{
			{Stream: labels, Values: values},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal push request: %w", err)
	}

	if len(body) > maxBufferBytes {
		return ErrBufferExceeded
	}

	url := buildPushURL(p.target)

	var lastErr error
	for attempt := range p.maxRetries {
		if err := ctx.Err(); err != nil {
			return err
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = err
			if attempt < p.maxRetries-1 {
				if p.onRetry != nil {
					p.onRetry()
				}
				backoff(ctx, attempt, p.maxBackoff)
			}
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		lastErr = fmt.Errorf("push failed: HTTP %d", resp.StatusCode)

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return lastErr // client error, no retry
		}

		if attempt < p.maxRetries-1 {
			if p.onRetry != nil {
				p.onRetry()
			}
			backoff(ctx, attempt, p.maxBackoff)
		}
	}

	return lastErr
}

// ErrBufferExceeded is returned when the serialized payload exceeds the buffer limit.
var ErrBufferExceeded = fmt.Errorf("payload exceeds %d byte buffer limit", maxBufferBytes)

// buildPushURL constructs the push endpoint URL from a target address.
// Targets with an explicit scheme (http:// or https://) are used as-is.
// Plain host:port targets default to http://.
func buildPushURL(target string) string {
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		return strings.TrimRight(target, "/") + pushPath
	}
	return "http://" + target + pushPath
}

// TargetURL constructs a URL for the given target and path, respecting scheme prefixes.
func TargetURL(target, path string) string {
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		return strings.TrimRight(target, "/") + path
	}
	return "http://" + target + path
}

func backoff(ctx context.Context, attempt int, maxBackoff time.Duration) {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > maxBackoff {
		d = maxBackoff
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}
