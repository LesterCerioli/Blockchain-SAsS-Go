package queue

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

type topic struct {
	handlers []ports.EventHandler
	jobs     chan []byte
	cancel   context.CancelFunc
}

type MemoryQueue struct {
	mu      sync.RWMutex
	topics  map[string]*topic
	workers int
	buffer  int
	closed  bool
	wg      sync.WaitGroup
}

func NewMemoryQueue(workers, buffer int) *MemoryQueue {
	if workers <= 0 {
		workers = 4
	}
	if buffer <= 0 {
		buffer = 1024
	}
	return &MemoryQueue{
		topics:  make(map[string]*topic),
		workers: workers,
		buffer:  buffer,
	}
}

func (q *MemoryQueue) Publish(ctx context.Context, topicName string, payload []byte) error {
	q.mu.RLock()
	t, ok := q.topics[topicName]
	closed := q.closed
	q.mu.RUnlock()
	if closed {
		return errors.New("queue is closed")
	}
	if !ok {
		return nil
	}
	select {
	case t.jobs <- payload:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return errors.New("queue publish timeout")
	}
}

func (q *MemoryQueue) Subscribe(topicName string, handler ports.EventHandler) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return errors.New("queue is closed")
	}
	t, ok := q.topics[topicName]
	if !ok {
		ctx, cancel := context.WithCancel(context.Background())
		t = &topic{jobs: make(chan []byte, q.buffer), cancel: cancel}
		q.topics[topicName] = t
		for i := 0; i < q.workers; i++ {
			q.wg.Add(1)
			go q.worker(ctx, t.jobs)
		}
	}
	t.handlers = append(t.handlers, handler)
	return nil
}

func (q *MemoryQueue) worker(ctx context.Context, jobs <-chan []byte) {
	defer q.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case payload, ok := <-jobs:
			if !ok {
				return
			}
			q.mu.RLock()
			var handlers []ports.EventHandler
			for _, t := range q.topics {
				if t.jobs == jobs {
					handlers = append(handlers, t.handlers...)
					break
				}
			}
			q.mu.RUnlock()
			for _, handler := range handlers {
				_ = handler(ctx, payload)
			}
		}
	}
}

func (q *MemoryQueue) Close() error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil
	}
	q.closed = true
	for _, t := range q.topics {
		t.cancel()
	}
	q.mu.Unlock()
	q.wg.Wait()
	return nil
}

var _ ports.Queue = (*MemoryQueue)(nil)
