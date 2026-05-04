package harnas

import "sync"

type HookHandler func(ctx map[string]any) any
type HookErrorPolicy string

const (
	HookErrorIsolate  HookErrorPolicy = "isolate"
	HookErrorFailTurn HookErrorPolicy = "fail_turn"
)

type HookOptions struct {
	OnError HookErrorPolicy
	Name    string
	Source  string
}

type TurnFailed struct {
	Message string
}

func (t TurnFailed) Error() string { return t.Message }

type Hooks struct {
	mu       sync.Mutex
	handlers map[string][]HookHandler
	metadata map[uintptr]HookOptions
}

func NewHooks() *Hooks {
	return &Hooks{handlers: map[string][]HookHandler{}, metadata: map[uintptr]HookOptions{}}
}

func (h *Hooks) On(point string, handler HookHandler) HookHandler {
	return h.OnWithOptions(point, handler, HookOptions{})
}

func (h *Hooks) OnWithOptions(point string, handler HookHandler, options HookOptions) HookHandler {
	h.mu.Lock()
	defer h.mu.Unlock()
	if options.OnError == "" {
		options.OnError = HookErrorIsolate
	}
	if options.Name == "" {
		options.Name = "harnas.HookHandler"
	}
	if options.Source == "" {
		options.Source = "hook"
	}
	h.handlers[point] = append(h.handlers[point], handler)
	h.metadata[funcValuePointer(handler)] = options
	return handler
}

func (h *Hooks) Invoke(point string, ctx map[string]any) []any {
	h.mu.Lock()
	handlers := append([]HookHandler(nil), h.handlers[point]...)
	metadata := map[uintptr]HookOptions{}
	for key, value := range h.metadata {
		metadata[key] = value
	}
	h.mu.Unlock()

	returns := []any{}
	for _, handler := range handlers {
		value, ok := invokeHookHandler(point, handler, ctx, metadata[funcValuePointer(handler)])
		if ok && value != nil {
			returns = append(returns, value)
		}
	}
	return returns
}

func (h *Hooks) Off(point string, handler HookHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for index, existing := range h.handlers[point] {
		if funcEqualHook(existing, handler) {
			h.handlers[point] = append(h.handlers[point][:index], h.handlers[point][index+1:]...)
			delete(h.metadata, funcValuePointer(handler))
			return
		}
	}
}

func (h *Hooks) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers = map[string][]HookHandler{}
	h.metadata = map[uintptr]HookOptions{}
}

func (h *Hooks) Handlers() map[string][]HookHandler {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string][]HookHandler{}
	for point, handlers := range h.handlers {
		out[point] = append([]HookHandler(nil), handlers...)
	}
	return out
}

func invokeHookHandler(point string, handler HookHandler, ctx map[string]any, options HookOptions) (value any, ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if session, typed := ctx["session"].(*Session); typed && session.Observation != nil {
				session.Observation.Emit("hook_handler_failed", map[string]any{
					"point":   point,
					"handler": options.Name,
					"error":   recovered,
				})
			}
			if options.OnError == HookErrorFailTurn {
				if session, typed := ctx["session"].(*Session); typed {
					message := recoveredMessage(recovered)
					session.Log.Append(EventRuntimeError, map[string]any{
						"source":      options.Source,
						"handler":     options.Name,
						"error_class": recoveredClass(recovered),
						"message":     message,
						"terminal":    true,
					})
					panic(TurnFailed{Message: message})
				}
			}
			value = nil
			ok = false
		}
	}()
	return handler(ctx), true
}

func recoveredMessage(recovered any) string {
	if err, ok := recovered.(error); ok {
		return err.Error()
	}
	if text, ok := recovered.(string); ok {
		return text
	}
	return "hook handler failed"
}

func recoveredClass(recovered any) string {
	if _, ok := recovered.(error); ok {
		return "RuntimeError"
	}
	return "RuntimeError"
}

func funcEqualHook(left, right HookHandler) bool {
	return funcValuePointer(left) == funcValuePointer(right)
}
