package notify

import (
	"fmt"
	"net/http"
	"strings"
)

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
func (n *Notifier) Send(message string) error {
	if n.Topic == "" {
		return nil
	}

	url := n.BaseURL + "/" + n.Topic
	resp, err := http.Post(url, "text/plain", strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("notify: server returned %d", resp.StatusCode)
	}
	return nil
}
