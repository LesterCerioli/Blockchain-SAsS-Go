package notifications

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

type EmailOptions struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

type EmailAdapter struct {
	options EmailOptions
}

func NewEmailAdapter(options EmailOptions) *EmailAdapter {
	return &EmailAdapter{options: options}
}

func (a *EmailAdapter) Channel() ports.NotificationChannel { return ports.ChannelEmail }

func (a *EmailAdapter) Send(_ context.Context, notification ports.Notification) error {
	if !a.options.Enabled {
		return fmt.Errorf("email notifications are disabled")
	}
	if notification.Recipient == "" {
		return fmt.Errorf("email recipient is required")
	}
	from := a.options.From
	if from == "" {
		from = "no-reply@example.com"
	}
	headers := []string{
		"From: " + from,
		"To: " + notification.Recipient,
		"Subject: " + sanitizeHeader(notification.Subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	message := []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + notification.Body + "\r\n")
	addr := fmt.Sprintf("%s:%d", a.options.Host, a.options.Port)
	var auth smtp.Auth
	if a.options.Username != "" {
		auth = smtp.PlainAuth("", a.options.Username, a.options.Password, a.options.Host)
	}
	if err := smtp.SendMail(addr, auth, from, []string{notification.Recipient}, message); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}
