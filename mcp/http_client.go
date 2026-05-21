package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	harnas "github.com/Tedo-ai/harnas-go"
)

type HTTPClient struct {
	url     string
	timeout time.Duration
	headers map[string]string
	nextID  atomic.Int64
	state   clientState
}

type HTTPClientOption func(*HTTPClient)

func WithHTTPTimeout(timeout time.Duration) HTTPClientOption {
	return func(c *HTTPClient) { c.timeout = timeout }
}

func WithHeaders(headers map[string]string) HTTPClientOption {
	return func(c *HTTPClient) {
		c.headers = map[string]string{}
		for key, value := range headers {
			c.headers[key] = value
		}
	}
}

func NewHTTPClient(url, serverName string, opts ...HTTPClientOption) *HTTPClient {
	client := &HTTPClient{
		url:     url,
		timeout: DefaultTimeout,
		headers: map[string]string{},
		state:   clientState{serverName: serverName},
	}
	for _, opt := range opts {
		opt(client)
	}
	client.nextID.Store(1)
	return client
}

func (c *HTTPClient) InitializeSession(ctx context.Context) error {
	if _, err := c.request(ctx, "initialize", initializeParams()); err != nil {
		return err
	}
	return c.notify(ctx, "notifications/initialized")
}

func (c *HTTPClient) Tools(ctx context.Context) ([]harnas.ToolSpec, error) {
	return c.state.loadTools(ctx, c), nil
}

func (c *HTTPClient) ToolHandlers() map[string]harnas.ConfiguredToolHandler {
	return c.state.toolHandlers(c)
}

func (c *HTTPClient) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	if err := c.state.degradedCallError(); err != nil {
		return "", err
	}
	result, err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return "", err
	}
	content, _ := result["content"].([]any)
	return Flatten(contentItems(content)), nil
}

func (c *HTTPClient) Close() error {
	return nil
}

func (c *HTTPClient) listTools(ctx context.Context) ([]map[string]any, error) {
	result, err := c.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	rawTools, _ := result["tools"].([]any)
	tools := make([]map[string]any, 0, len(rawTools))
	for _, raw := range rawTools {
		if tool, ok := raw.(map[string]any); ok {
			tools = append(tools, tool)
		}
	}
	return tools, nil
}

func (c *HTTPClient) request(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	id := c.nextID.Add(1) - 1
	body, err := c.post(ctx, requestPayload(id, method, params))
	if err != nil {
		return nil, err
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, TransportError{Message: "malformed JSON response", Cause: err}
	}
	if response["error"] != nil {
		return nil, TransportError{Message: fmt.Sprint(response["error"])}
	}
	result, _ := response["result"].(map[string]any)
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
}

func (c *HTTPClient) notify(ctx context.Context, method string) error {
	_, err := c.post(ctx, requestPayload(nil, method, map[string]any{}))
	return err
}

func (c *HTTPClient) post(ctx context.Context, payload map[string]any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.url, bytes.NewReader(data))
	if err != nil {
		return nil, TransportError{Message: "invalid MCP URL", Cause: err}
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range c.headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		if requestCtx.Err() != nil {
			return nil, TimeoutError{TransportError{Message: "timed out waiting for MCP response", Cause: err}}
		}
		return nil, TransportError{Message: "MCP HTTP request failed", Cause: err}
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, TransportError{Message: fmt.Sprintf("HTTP %d: %s", response.StatusCode, string(body))}
	}
	return body, nil
}

func contentItems(raw []any) []map[string]any {
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if mapped, ok := item.(map[string]any); ok {
			items = append(items, mapped)
		}
	}
	return items
}
