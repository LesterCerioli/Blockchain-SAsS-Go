package ports

import (
	"context"
	"fmt"
)

type NotificationChannel string

const (
	ChannelEmail   NotificationChannel = "email"
	ChannelSMS     NotificationChannel = "sms"
	ChannelWebhook NotificationChannel = "webhook"
)

type Notification struct {
	Channel   NotificationChannel    `json:"channel"`
	Recipient string                 `json:"recipient"`
	Subject   string                 `json:"subject"`
	Body      string                 `json:"body"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Headers   map[string]string      `json:"headers,omitempty"`
	Secret    string                 `json:"-"`
}

func (n Notification) DataString(key string) string {
	if n.Data == nil {
		return ""
	}
	value, ok := n.Data[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

type NotificationPort interface {
	Channel() NotificationChannel
	Send(ctx context.Context, notification Notification) error
}
