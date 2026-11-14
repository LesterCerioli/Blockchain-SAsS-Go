package cache

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

type RedisCache struct {
	addr     string
	password string
	db       int
	timeout  time.Duration

	mu   sync.Mutex
	conn net.Conn
	rd   *bufio.Reader
	wr   *bufio.Writer
}

func NewRedisCache(addr, password string, db int) *RedisCache {
	return &RedisCache{addr: addr, password: password, db: db, timeout: 5 * time.Second}
}

func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	reply, err := c.do(ctx, "GET", key)
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, ErrCacheMiss
	}
	value, ok := reply.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected redis GET reply %T", reply)
	}
	return []byte(value), nil
}

func (c *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	var err error
	if ttl > 0 {
		_, err = c.do(ctx, "SET", key, string(value), "PX", strconv.FormatInt(ttl.Milliseconds(), 10))
	} else {
		_, err = c.do(ctx, "SET", key, string(value))
	}
	return err
}

func (c *RedisCache) Delete(ctx context.Context, key string) error {
	_, err := c.do(ctx, "DEL", key)
	return err
}

func (c *RedisCache) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	reply, err := c.do(ctx, "INCR", key)
	if err != nil {
		return 0, err
	}
	value, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("unexpected redis INCR reply %T", reply)
	}
	if ttl > 0 && value == 1 {
		if _, err := c.do(ctx, "PEXPIRE", key, strconv.FormatInt(ttl.Milliseconds(), 10)); err != nil {
			return value, err
		}
	}
	return value, nil
}

func (c *RedisCache) Ping(ctx context.Context) error {
	_, err := c.do(ctx, "PING")
	return err
}

func (c *RedisCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

func (c *RedisCache) do(ctx context.Context, args ...string) (interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.connectLocked(ctx); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.conn.SetDeadline(deadline)

	if err := writeCommand(c.wr, args); err != nil {
		c.resetLocked()
		return nil, err
	}
	if err := c.wr.Flush(); err != nil {
		c.resetLocked()
		return nil, err
	}
	reply, err := readReply(c.rd)
	if err != nil {
		c.resetLocked()
		return nil, err
	}
	return reply, nil
}

func (c *RedisCache) connectLocked(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	dialer := net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return fmt.Errorf("redis dial: %w", err)
	}
	c.conn = conn
	c.rd = bufio.NewReader(conn)
	c.wr = bufio.NewWriter(conn)

	if c.password != "" {
		if err := c.simpleCommandLocked("AUTH", c.password); err != nil {
			c.resetLocked()
			return err
		}
	}
	if c.db != 0 {
		if err := c.simpleCommandLocked("SELECT", strconv.Itoa(c.db)); err != nil {
			c.resetLocked()
			return err
		}
	}
	return nil
}

func (c *RedisCache) simpleCommandLocked(args ...string) error {
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	if err := writeCommand(c.wr, args); err != nil {
		return err
	}
	if err := c.wr.Flush(); err != nil {
		return err
	}
	_, err := readReply(c.rd)
	return err
}

func (c *RedisCache) resetLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
	c.rd = nil
	c.wr = nil
}

func writeCommand(w *bufio.Writer, args []string) error {
	if _, err := fmt.Fprintf(w, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return nil
}

type redisError string

func (e redisError) Error() string { return string(e) }

func readReply(r *bufio.Reader) (interface{}, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if line == "" {
		return nil, io.ErrUnexpectedEOF
	}
	switch line[0] {
	case '+':
		return line[1:], nil
	case '-':
		return nil, redisError(line[1:])
	case ':':
		return strconv.ParseInt(line[1:], 10, 64)
	case '$':
		length, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, nil
		}
		buf := make([]byte, length+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:length]), nil
	case '*':
		count, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, nil
		}
		items := make([]interface{}, 0, count)
		for i := 0; i < count; i++ {
			item, err := readReply(r)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	default:
		return nil, errors.New("unknown redis reply")
	}
}

var _ ports.Cache = (*RedisCache)(nil)
