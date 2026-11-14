package queue

import (
	"context"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

func TestMemoryQueuePublishSubscribe(t *testing.T) {
	queue := NewMemoryQueue(1, 1)
	defer queue.Close()

	received := make(chan []byte, 1)
	if err := queue.Subscribe("events", func(_ context.Context, payload []byte) error {
		received <- payload
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := queue.Publish(context.Background(), "events", []byte("hello")); err != nil {
		t.Fatal(err)
	}

	select {
	case payload := <-received:
		if string(payload) != "hello" {
			t.Fatalf("payload = %q", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("handler was not called")
	}
}

func TestMemoryQueueDefaultsAndNoSubscribers(t *testing.T) {
	queue := NewMemoryQueue(0, 0)
	if queue.workers != 4 || queue.buffer != 1024 {
		t.Fatalf("workers = %d buffer = %d", queue.workers, queue.buffer)
	}
	if err := queue.Publish(context.Background(), "unknown", []byte("ignored")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestMemoryQueueClosed(t *testing.T) {
	queue := NewMemoryQueue(1, 1)
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	if err := queue.Publish(context.Background(), "events", []byte("payload")); err == nil {
		t.Fatal("expected publish error on closed queue")
	}
	if err := queue.Subscribe("events", func(context.Context, []byte) error { return nil }); err == nil {
		t.Fatal("expected subscribe error on closed queue")
	}
}

func TestMemoryQueueContextCancelled(t *testing.T) {
	queue := NewMemoryQueue(1, 1)
	defer queue.Close()
	block := make(chan struct{})
	if err := queue.Subscribe("events", func(context.Context, []byte) error {
		<-block
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := queue.Publish(context.Background(), "events", []byte("first")); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := queue.Publish(ctx, "events", []byte("second"))
	if err == nil {
		t.Fatal("expected context error")
	}
	close(block)
	_ = queue.Close()
	_ = ports.EventHandler(nil)
}
