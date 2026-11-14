package notifications

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
)

type WebhookOptions struct {
	Timeout         time.Duration
	SignatureHeader string
	UserAgent       string
}

type WebhookAdapter struct {
	client          *http.Client
	signatureHeader string
	userAgent       string
}

func NewWebhookAdapter(options WebhookOptions) *WebhookAdapter {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	header := options.SignatureHeader
	if header == "" {
		header = "X-Signature"
	}
	userAgent := options.UserAgent
	if userAgent == "" {
		userAgent = "blockchain-payment-gateway/1.0"
	}
	return &WebhookAdapter{
		client:          &http.Client{Timeout: timeout},
		signatureHeader: header,
		userAgent:       userAgent,
	}
}

func (a *WebhookAdapter) Channel() ports.NotificationChannel { return ports.ChannelWebhook }

type webhookResultError struct {
	StatusCode int
	Body       string
}

func (e *webhookResultError) Error() string {
	return fmt.Sprintf("webhook returned status %d: %s", e.StatusCode, e.Body)
}

func (e *webhookResultError) HTTPStatusCode() int { return e.StatusCode }

func (a *WebhookAdapter) Send(ctx context.Context, notification ports.Notification) error {
	if notification.Recipient == "" {
		return fmt.Errorf("webhook url is required")
	}
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	body := []byte(notification.Body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, notification.Recipient, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", a.userAgent)
	req.Header.Set("X-Webhook-Timestamp", timestamp)
	req.Header.Set("X-Webhook-Id", notification.DataString("delivery_id"))
	if notification.Secret != "" {
		req.Header.Set(a.signatureHeader, "sha256="+utils.SignWebhook(notification.Secret, timestamp, body))
	}
	for key, value := range notification.Headers {
		req.Header.Set(key, value)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("deliver webhook: %w", err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &webhookResultError{StatusCode: resp.StatusCode, Body: string(responseBody)}
	}
	return nil
}
