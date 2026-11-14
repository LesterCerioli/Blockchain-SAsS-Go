package blockchain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

type JSONRPCClient struct {
	url    string
	user   string
	pass   string
	client *http.Client
	id     uint64
}

func NewJSONRPCClient(url, user, pass string, timeout time.Duration) *JSONRPCClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &JSONRPCClient{
		url:    url,
		user:   user,
		pass:   pass,
		client: &http.Client{Timeout: timeout},
	}
}

func (c *JSONRPCClient) Call(ctx context.Context, method string, params interface{}, result interface{}) error {
	requestID := atomic.AddUint64(&c.id, 1)
	body, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.user != "" || c.pass != "" {
		req.SetBasicAuth(c.user, c.pass)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("rpc request failed: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read rpc response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("rpc http status %d: %s", resp.StatusCode, string(payload))
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("decode rpc response: %w", err)
	}
	if envelope.Error != nil {
		return envelope.Error
	}
	if result == nil {
		return nil
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return nil
	}
	return json.Unmarshal(envelope.Result, result)
}
