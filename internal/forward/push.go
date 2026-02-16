package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	maxBufferBytes = 1 << 20 // 1MB
	maxRetries     = 3
	pushPath       = "/loki/api/v1/push"
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
	target string
	client *http.Client
}

// NewPusher creates a Pusher targeting the given receiver address.
func NewPusher(target string) *Pusher {
	return NewPusherWithClient(target, &http.Client{Timeout: 10 * time.Second})
}

// NewPusherWithClient creates a Pusher with a custom HTTP client (useful for tests).
func NewPusherWithClient(target string, client *http.Client) *Pusher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Pusher{
		target: target,
		client: client,
	}
}

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

	url := "http://" + p.target + pushPath

	var lastErr error
	for attempt := range maxRetries {
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
			backoff(ctx, attempt)
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

		backoff(ctx, attempt)
	}

	return lastErr
}

// ErrBufferExceeded is returned when the serialized payload exceeds the buffer limit.
var ErrBufferExceeded = fmt.Errorf("payload exceeds %d byte buffer limit", maxBufferBytes)

func backoff(ctx context.Context, attempt int) {
	d := time.Duration(1<<uint(attempt)) * time.Second
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}
