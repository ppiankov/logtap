package recv

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const webhookTimeout = 5 * time.Second

// WebhookEvent is the JSON payload sent to webhook URLs.
type WebhookEvent struct {
	Event     string        `json:"event"`
	Timestamp time.Time     `json:"timestamp"`
	Dir       string        `json:"dir,omitempty"`
	Stats     *WebhookStats `json:"stats,omitempty"`
	Detail    string        `json:"detail,omitempty"`
}

// WebhookStats contains capture metrics included in webhook payloads.
type WebhookStats struct {
	LinesWritten int64 `json:"lines_written"`
	BytesWritten int64 `json:"bytes_written"`
	DiskUsage    int64 `json:"disk_usage"`
	DiskCap      int64 `json:"disk_cap"`
}

// WebhookDispatcher sends fire-and-forget HTTP POST notifications.
type WebhookDispatcher struct {
	urls      []string
	events    map[string]bool
	client    *http.Client
	authMode  string
	authValue string
}

// ParseWebhookAuth validates and splits an auth spec into mode and value.
// Accepted formats: "" (no auth), "bearer:<token>", "hmac-sha256:<secret>".
func ParseWebhookAuth(spec string) (mode, value string, err error) {
	if spec == "" {
		return "", "", nil
	}
	idx := strings.IndexByte(spec, ':')
	if idx < 0 || idx == len(spec)-1 {
		return "", "", fmt.Errorf("invalid webhook auth format: expected \"bearer:<token>\" or \"hmac-sha256:<secret>\"")
	}
	mode = spec[:idx]
	value = spec[idx+1:]
	switch mode {
	case "bearer", "hmac-sha256":
		return mode, value, nil
	default:
		return "", "", fmt.Errorf("unsupported webhook auth mode: %q (use bearer or hmac-sha256)", mode)
	}
}

// NewWebhookDispatcher creates a dispatcher for the given URLs and event filter.
// If eventFilter is empty, all events are accepted.
// authSpec controls request authentication: "" for none, "bearer:<token>", or "hmac-sha256:<secret>".
func NewWebhookDispatcher(urls []string, eventFilter []string, authSpec string) (*WebhookDispatcher, error) {
	if len(urls) == 0 {
		return nil, nil
	}

	mode, val, err := ParseWebhookAuth(authSpec)
	if err != nil {
		return nil, err
	}

	events := make(map[string]bool)
	for _, e := range eventFilter {
		events[e] = true
	}

	return &WebhookDispatcher{
		urls:      urls,
		events:    events,
		client:    &http.Client{Timeout: webhookTimeout},
		authMode:  mode,
		authValue: val,
	}, nil
}

// Fire sends the event to all configured webhooks in background goroutines.
// It returns immediately (non-blocking). Errors are silently dropped.
func (d *WebhookDispatcher) Fire(evt WebhookEvent) {
	if d == nil || len(d.urls) == 0 {
		return
	}

	if len(d.events) > 0 && !d.events[evt.Event] {
		return
	}

	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	data, err := json.Marshal(evt)
	if err != nil {
		return
	}

	for _, url := range d.urls {
		go d.post(url, data)
	}
}

func (d *WebhookDispatcher) post(url string, data []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	switch d.authMode {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+d.authValue)
	case "hmac-sha256":
		mac := hmac.New(sha256.New, []byte(d.authValue))
		_, _ = mac.Write(data)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Logtap-Signature", "sha256="+sig)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
