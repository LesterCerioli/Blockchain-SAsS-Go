package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

type SMSOptions struct {
	Enabled  bool
	Endpoint string
	Token    string
	From     string
	Timeout  time.Duration
}

type SMSAdapter struct {
	options SMSOptions
	client  *http.Client
}

func NewSMSAdapter(options SMSOptions) *SMSAdapter {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &SMSAdapter{options: options, client: &http.Client{Timeout: timeout}}
}

func (a *SMSAdapter) Channel() ports.NotificationChannel { return ports.ChannelSMS }

func (a *SMSAdapter) Send(ctx context.Context, notification ports.Notification) error {
	if !a.options.Enabled {
		return fmt.Errorf("sms notifications are disabled")
	}
	if a.options.Endpoint == "" {
		return fmt.Errorf("sms endpoint is not configured")
	}
	payload, _ := json.Marshal(map[string]string{
		"to":      notification.Recipient,
		"from":    a.options.From,
		"message": notification.Body,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.options.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.options.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.options.Token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("send sms: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return fmt.Errorf("sms provider status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
