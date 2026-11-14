package notifications

import (
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

type Options struct {
	Email   EmailOptions
	SMS     SMSOptions
	Webhook WebhookOptions
}

func NewNotifiers(options Options) map[ports.NotificationChannel]ports.NotificationPort {
	notifiers := map[ports.NotificationChannel]ports.NotificationPort{
		ports.ChannelWebhook: NewWebhookAdapter(options.Webhook),
	}
	if options.Email.Enabled {
		notifiers[ports.ChannelEmail] = NewEmailAdapter(options.Email)
	}
	if options.SMS.Enabled {
		sms := options.SMS
		if sms.Timeout == 0 {
			sms.Timeout = 10 * time.Second
		}
		notifiers[ports.ChannelSMS] = NewSMSAdapter(sms)
	}
	return notifiers
}
