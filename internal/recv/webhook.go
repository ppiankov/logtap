package recv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
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
	urls   []string
	events map[string]bool
	client *http.Client
}

// NewWebhookDispatcher creates a dispatcher for the given URLs and event filter.
// If eventFilter is empty, all events are accepted.
func NewWebhookDispatcher(urls []string, eventFilter []string) *WebhookDispatcher {
	if len(urls) == 0 {
		return nil
	}

	events := make(map[string]bool)
	for _, e := range eventFilter {
		events[e] = true
	}

	return &WebhookDispatcher{
		urls:   urls,
		events: events,
		client: &http.Client{Timeout: webhookTimeout},
	}
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

	resp, err := d.client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
