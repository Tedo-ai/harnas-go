package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	harnas "github.com/Tedo-ai/harnas-go"
)

type StdioClient struct {
	command string
	args    []string
	env     map[string]string
	timeout time.Duration
	nextID  atomic.Int64
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	pending map[int64]chan stdioResponse
	mu      sync.Mutex
	closed  bool
	state   clientState
}

type stdioResponse struct {
	body map[string]any
	err  error
}

type StdioClientOption func(*StdioClient)

func WithStdioTimeout(timeout time.Duration) StdioClientOption {
	return func(c *StdioClient) { c.timeout = timeout }
}

func WithEnv(env map[string]string) StdioClientOption {
	return func(c *StdioClient) {
		c.env = map[string]string{}
		for key, value := range env {
			c.env[key] = value
		}
	}
}

func NewStdioClient(command string, args []string, serverName string, opts ...StdioClientOption) (*StdioClient, error) {
	client := &StdioClient{
		command: command,
		args:    append([]string(nil), args...),
		env:     map[string]string{},
		timeout: DefaultTimeout,
		pending: map[int64]chan stdioResponse{},
		state:   clientState{serverName: serverName},
	}
	for _, opt := range opts {
		opt(client)
	}
	client.nextID.Store(1)
	if err := client.spawn(); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *StdioClient) InitializeSession(ctx context.Context) error {
	if _, err := c.request(ctx, "initialize", initializeParams()); err != nil {
		return err
	}
	return c.notify("notifications/initialized")
}

func (c *StdioClient) Tools(ctx context.Context) ([]harnas.ToolSpec, error) {
	return c.state.loadTools(ctx, c), nil
}

func (c *StdioClient) ToolHandlers() map[string]harnas.ConfiguredToolHandler {
	return c.state.toolHandlers(c)
}

func (c *StdioClient) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
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

func (c *StdioClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	c.mu.Unlock()

	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	_ = terminateStdioProcess(c.cmd, false)
	done := make(chan struct{})
	go func() {
		_ = c.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = terminateStdioProcess(c.cmd, true)
	}
	return nil
}

func (c *StdioClient) listTools(ctx context.Context) ([]map[string]any, error) {
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

func (c *StdioClient) spawn() error {
	c.cmd = exec.Command(c.command, c.args...)
	configureStdioCommand(c.cmd)
	c.cmd.Env = os.Environ()
	for key, value := range c.env {
		c.cmd.Env = append(c.cmd.Env, key+"="+value)
	}
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return StartupError{TransportError{Message: "MCP subprocess stdin failed", Cause: err}}
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return StartupError{TransportError{Message: "MCP subprocess stdout failed", Cause: err}}
	}
	if err := c.cmd.Start(); err != nil {
		return StartupError{TransportError{Message: "MCP subprocess failed to start", Cause: err}}
	}
	c.stdin = stdin
	go c.readLoop(stdout)
	return nil
}

func (c *StdioClient) request(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	id := c.nextID.Add(1) - 1
	ch := make(chan stdioResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.write(requestPayload(id, method, params)); err != nil {
		c.deletePending(id)
		return nil, err
	}
	timeout := c.timeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}
	select {
	case response := <-ch:
		if response.err != nil {
			return nil, response.err
		}
		if response.body["error"] != nil {
			return nil, TransportError{Message: fmt.Sprint(response.body["error"])}
		}
		result, _ := response.body["result"].(map[string]any)
		if result == nil {
			result = map[string]any{}
		}
		return result, nil
	case <-time.After(timeout):
		c.deletePending(id)
		return nil, TimeoutError{TransportError{Message: fmt.Sprintf("timed out waiting for MCP response %d", id)}}
	case <-ctx.Done():
		c.deletePending(id)
		return nil, TimeoutError{TransportError{Message: "timed out waiting for MCP response", Cause: ctx.Err()}}
	}
}

func (c *StdioClient) notify(method string) error {
	return c.write(requestPayload(nil, method, map[string]any{}))
}

func (c *StdioClient) write(payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return TransportError{Message: "MCP subprocess is closed"}
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return TransportError{Message: "MCP subprocess is not writable", Cause: err}
	}
	return nil
}

func (c *StdioClient) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		var response map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			c.failPending(TransportError{Message: "malformed JSON response", Cause: err})
			return
		}
		idFloat, ok := response["id"].(float64)
		if !ok {
			continue
		}
		id := int64(idFloat)
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- stdioResponse{body: response}
		}
	}
	if err := scanner.Err(); err != nil {
		c.failPending(TransportError{Message: "MCP subprocess stdout failed", Cause: err})
		return
	}
	c.failPending(TransportError{Message: "MCP subprocess exited"})
}

func (c *StdioClient) deletePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *StdioClient) failPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for id, ch := range c.pending {
		ch <- stdioResponse{err: err}
		delete(c.pending, id)
	}
}
