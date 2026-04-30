package harnas

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

func Timed(handler ToolHandler) ToolHandler {
	return func(args map[string]any) (string, error) {
		_ = time.Now()
		return handler(args)
	}
}

func Logged(handler ToolHandler, writer io.Writer) ToolHandler {
	return func(args map[string]any) (string, error) {
		if writer != nil {
			fmt.Fprintf(writer, "tool start args=%v\n", args)
		}
		output, err := handler(args)
		if writer != nil {
			if err != nil {
				fmt.Fprintf(writer, "tool error error=%v\n", err)
			} else {
				fmt.Fprintf(writer, "tool ok bytes=%d\n", len([]byte(output)))
			}
		}
		return output, err
	}
}

func Retried(handler ToolHandler, attempts int, retryable func(error) bool) ToolHandler {
	if attempts < 1 {
		attempts = 1
	}
	return func(args map[string]any) (string, error) {
		var last error
		for i := 0; i < attempts; i++ {
			output, err := handler(args)
			if err == nil {
				return output, nil
			}
			last = err
			if retryable != nil && !retryable(err) {
				break
			}
		}
		return "", last
	}
}

type RateLimiter struct {
	PerMinute int
	mu        sync.Mutex
	window    time.Time
	count     int
}

func (r *RateLimiter) Wrap(handler ToolHandler) ToolHandler {
	return func(args map[string]any) (string, error) {
		if err := r.admit(time.Now()); err != nil {
			return "", err
		}
		return handler(args)
	}
}

func (r *RateLimiter) admit(now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.PerMinute <= 0 {
		return fmt.Errorf("per_minute must be positive")
	}
	if r.window.IsZero() || now.Sub(r.window) >= time.Minute {
		r.window = now
		r.count = 0
	}
	if r.count >= r.PerMinute {
		return fmt.Errorf("rate limit exceeded")
	}
	r.count++
	return nil
}

type StaleReadGuard struct {
	Log         *Log
	Strict      bool
	RequireRead bool
}

func (g StaleReadGuard) WrapRead(handler ToolHandler) ToolHandler {
	return func(args map[string]any) (string, error) {
		output, err := handler(args)
		if err != nil {
			return "", err
		}
		path := stringValue(args["path"])
		if path != "" && g.Log != nil {
			g.Log.Append(EventAnnotation, map[string]any{
				"kind": "stale_read_guard.hash",
				"data": map[string]any{"path": path, "sha256": sha256Hex(output)},
			})
		}
		return output, nil
	}
}

func (g StaleReadGuard) WrapEdit(handler ToolHandler) ToolHandler {
	return func(args map[string]any) (string, error) {
		path := stringValue(args["path"])
		if path != "" && g.Log != nil {
			if err := g.check(path); err != nil {
				return "", err
			}
		}
		return handler(args)
	}
}

func (g StaleReadGuard) check(path string) error {
	lastHash := ""
	for _, event := range g.Log.Events() {
		if event.Type != EventAnnotation || event.Payload["kind"] != "stale_read_guard.hash" {
			continue
		}
		data := asMap(event.Payload["data"])
		if data["path"] == path {
			lastHash = stringValue(data["sha256"])
		}
	}
	if lastHash == "" {
		if g.RequireRead || g.Strict {
			return fmt.Errorf("stale read guard: %s was not read before edit", path)
		}
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if sha256Hex(string(content)) != lastHash {
		return fmt.Errorf("stale read guard: %s changed since last read", path)
	}
	return nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}
