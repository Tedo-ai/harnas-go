package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	harnas "github.com/Tedo-ai/harnas-go"
)

const (
	ProtocolVersion = "2024-11-05"
	ClientName      = "harnas-go"
	ClientVersion   = "0.15.0"
	DefaultTimeout  = 30 * time.Second
)

type Client interface {
	InitializeSession(ctx context.Context) error
	Tools(ctx context.Context) ([]harnas.ToolSpec, error)
	ToolHandlers() map[string]harnas.ConfiguredToolHandler
	CallTool(ctx context.Context, name string, arguments map[string]any) (string, error)
	Close() error
}

type ConnectOptions struct {
	URL        string
	Command    string
	Args       []string
	ServerName string
	Headers    map[string]string
	Timeout    time.Duration
	Env        map[string]string
}

func Connect(options ConnectOptions) (Client, error) {
	if options.ServerName == "" {
		return nil, errors.New("mcp.Connect: server name is required")
	}
	if options.URL != "" {
		opts := []HTTPClientOption{}
		if options.Timeout > 0 {
			opts = append(opts, WithHTTPTimeout(options.Timeout))
		}
		if options.Headers != nil {
			opts = append(opts, WithHeaders(options.Headers))
		}
		return NewHTTPClient(options.URL, options.ServerName, opts...), nil
	}
	if options.Command != "" {
		opts := []StdioClientOption{}
		if options.Timeout > 0 {
			opts = append(opts, WithStdioTimeout(options.Timeout))
		}
		if options.Env != nil {
			opts = append(opts, WithEnv(options.Env))
		}
		return NewStdioClient(options.Command, options.Args, options.ServerName, opts...)
	}
	return nil, errors.New("mcp.Connect: must provide either URL or Command")
}

type clientState struct {
	serverName    string
	mu            sync.Mutex
	toolsLoaded   bool
	tools         []harnas.ToolSpec
	degraded      bool
	degradedError error
}

func (s *clientState) toolHandlers(caller interface {
	CallTool(context.Context, string, map[string]any) (string, error)
}) map[string]harnas.ConfiguredToolHandler {
	return map[string]harnas.ConfiguredToolHandler{
		passthroughHandlerName(s.serverName): func(args map[string]any, config map[string]any) (string, error) {
			mcpName, _ := config["mcp_tool_name"].(string)
			return caller.CallTool(context.Background(), mcpName, args)
		},
	}
}

func (s *clientState) loadTools(ctx context.Context, loader interface {
	InitializeSession(context.Context) error
	listTools(context.Context) ([]map[string]any, error)
}) []harnas.ToolSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.toolsLoaded {
		return append([]harnas.ToolSpec(nil), s.tools...)
	}
	if err := loader.InitializeSession(ctx); err != nil {
		s.markDegraded(err)
		return nil
	}
	rawTools, err := loader.listTools(ctx)
	if err != nil {
		s.markDegraded(err)
		return nil
	}
	s.tools = make([]harnas.ToolSpec, 0, len(rawTools))
	for _, raw := range rawTools {
		s.tools = append(s.tools, ToolDescriptorFromMCP(raw, s.serverName))
	}
	s.toolsLoaded = true
	return append([]harnas.ToolSpec(nil), s.tools...)
}

func (s *clientState) markDegraded(err error) {
	s.degraded = true
	s.degradedError = err
	s.toolsLoaded = true
	fmt.Fprintf(os.Stderr, "Harnas MCP %s degraded: %T: %v\n", s.serverName, err, err)
}

func (s *clientState) degradedCallError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.degraded {
		return nil
	}
	return fmt.Errorf("MCP server %q is in degraded state; tools were not loaded", s.serverName)
}

func passthroughHandlerName(serverName string) string {
	return "mcp_passthrough." + serverName
}

func requestPayload(id any, method string, params map[string]any) map[string]any {
	payload := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if id != nil {
		payload["id"] = id
	}
	return payload
}

func initializeParams() map[string]any {
	return map[string]any{
		"protocolVersion": ProtocolVersion,
		"clientInfo":      map[string]any{"name": ClientName, "version": ClientVersion},
		"capabilities":    map[string]any{},
	}
}
