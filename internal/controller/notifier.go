package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Notifier sends messages to Slack or Discord via incoming webhooks.
type Notifier struct {
	webhookType string
	webhookURL  string
	httpClient  *http.Client
}

// NewNotifier creates a Notifier for the given webhook type ("slack" or "discord") and URL.
func NewNotifier(webhookType, webhookURL string) *Notifier {
	return &Notifier{
		webhookType: webhookType,
		webhookURL:  webhookURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

// SendWarning sends a pre-expiry warning notification.
func (n *Notifier) SendWarning(ctx context.Context, nsName string, expiresAt time.Time) error {
	remaining := time.Until(expiresAt).Round(time.Minute)
	msg := fmt.Sprintf("⚠️ BlinkNS Warning: namespace `%s` expires in %s (at %s)",
		nsName, remaining, expiresAt.UTC().Format("2006-01-02T15:04Z"))
	return n.send(ctx, msg)
}

// SendTerminated sends a notification when the namespace has been deleted.
func (n *Notifier) SendTerminated(ctx context.Context, nsName string) error {
	msg := fmt.Sprintf("🗑️ BlinkNS: namespace `%s` has been deleted (TTL expired)", nsName)
	return n.send(ctx, msg)
}

func (n *Notifier) send(ctx context.Context, message string) error {
	var payload map[string]string
	switch n.webhookType {
	case "slack":
		payload = map[string]string{"text": message}
	case "discord":
		payload = map[string]string{"content": message}
	default:
		return fmt.Errorf("unsupported webhook type: %q (use slack or discord)", n.webhookType)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
