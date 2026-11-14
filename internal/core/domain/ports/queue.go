package ports

import "context"

type EventHandler func(ctx context.Context, payload []byte) error

type Queue interface {
	Publish(ctx context.Context, topic string, payload []byte) error
	Subscribe(topic string, handler EventHandler) error
	Close() error
}
