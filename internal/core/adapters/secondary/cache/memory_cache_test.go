package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryCacheBasics(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryCache()

	if _, err := cache.Get(ctx, "missing"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("error = %v, want ErrCacheMiss", err)
	}
	if err := cache.Set(ctx, "key", []byte("value"), 0); err != nil {
		t.Fatal(err)
	}
	value, err := cache.Get(ctx, "key")
	if err != nil || string(value) != "value" {
		t.Fatalf("value = %q err = %v", value, err)
	}
	if err := cache.Delete(ctx, "key"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Get(ctx, "key"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("error = %v, want ErrCacheMiss", err)
	}
	if err := cache.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryCacheTTL(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryCache()
	if err := cache.Set(ctx, "short", []byte("value"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := cache.Get(ctx, "short"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("error = %v, want ErrCacheMiss", err)
	}
}

func TestMemoryCacheIncrement(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryCache()

	if value, err := cache.Increment(ctx, "counter", time.Minute); err != nil || value != 1 {
		t.Fatalf("value = %d err = %v", value, err)
	}
	if value, err := cache.Increment(ctx, "counter", time.Minute); err != nil || value != 2 {
		t.Fatalf("value = %d err = %v", value, err)
	}

	if err := cache.Set(ctx, "text", []byte("abc"), 0); err != nil {
		t.Fatal(err)
	}
	if value, err := cache.Increment(ctx, "text", 0); err != nil || value != 1 {
		t.Fatalf("value = %d err = %v", value, err)
	}

	if err := cache.Set(ctx, "expired", []byte("9"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if value, err := cache.Increment(ctx, "expired", 0); err != nil || value != 1 {
		t.Fatalf("value = %d err = %v", value, err)
	}
}

func TestItoa(t *testing.T) {
	cases := map[int64]string{0: "0", 1: "1", 42: "42", 1234567890: "1234567890"}
	for input, want := range cases {
		if got := itoa(input); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestNewCacheDrivers(t *testing.T) {
	if cache := NewCache(Options{Driver: "memory"}); cache == nil {
		t.Fatal("expected memory cache")
	}
	if _, ok := NewCache(Options{Driver: "memory"}).(*MemoryCache); !ok {
		t.Fatal("expected *MemoryCache")
	}
	if cache := NewCache(Options{Driver: "redis", Addr: "localhost:6379"}); cache == nil {
		t.Fatal("expected redis cache")
	}
}
