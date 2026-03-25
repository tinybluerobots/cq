package notify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrNotifyServerError is returned when the ntfy server returns an error status.
var ErrNotifyServerError = errors.New("notify: server error")

// Notifier sends push notifications via ntfy.sh.
type Notifier struct {
	BaseURL string
	Topic   string
}

// New creates a Notifier with the default ntfy.sh base URL.
func New(topic string) *Notifier {
	return &Notifier{
		BaseURL: "https://ntfy.sh",
		Topic:   topic,
	}
}

// Send posts a plain-text message to the configured topic.
// It is a no-op if Topic is empty.
func (n *Notifier) Send(ctx context.Context, message string) error {
	if n.Topic == "" {
		return nil
	}

	url := n.BaseURL + "/" + n.Topic

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("notify: %w", err)
	}

	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("notify: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("%w: status %d", ErrNotifyServerError, resp.StatusCode)
	}

	return nil
}
