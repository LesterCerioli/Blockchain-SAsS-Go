package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

var ErrCacheMiss = errors.New("cache miss")

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

type MemoryCache struct {
	mu    sync.RWMutex
	items map[string]memoryEntry
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{items: make(map[string]memoryEntry)}
}

func (c *MemoryCache) Get(_ context.Context, key string) ([]byte, error) {
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, ErrCacheMiss
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil, ErrCacheMiss
	}
	return entry.value, nil
}

func (c *MemoryCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := memoryEntry{value: value}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	c.items[key] = entry
	return nil
}

func (c *MemoryCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
	return nil
}

func (c *MemoryCache) Increment(_ context.Context, key string, ttl time.Duration) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[key]
	now := time.Now()
	if ok && !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
		ok = false
	}
	if !ok {
		entry = memoryEntry{value: []byte("0")}
		if ttl > 0 {
			entry.expiresAt = now.Add(ttl)
		}
	}
	var current int64
	if len(entry.value) > 0 {
		for _, b := range entry.value {
			if b < '0' || b > '9' {
				current = 0
				break
			}
			current = current*10 + int64(b-'0')
		}
	}
	current++
	entry.value = []byte(itoa(current))
	c.items[key] = entry
	return current, nil
}

func (c *MemoryCache) Ping(context.Context) error { return nil }

func (c *MemoryCache) Close() error { return nil }

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

var _ ports.Cache = (*MemoryCache)(nil)
