package harnas

import "sync"

type HookHandler func(ctx map[string]any) any

type Hooks struct {
	mu       sync.Mutex
	handlers map[string][]HookHandler
}

func NewHooks() *Hooks {
	return &Hooks{handlers: map[string][]HookHandler{}}
}

func (h *Hooks) On(point string, handler HookHandler) HookHandler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[point] = append(h.handlers[point], handler)
	return handler
}

func (h *Hooks) Invoke(point string, ctx map[string]any) []any {
	h.mu.Lock()
	handlers := append([]HookHandler(nil), h.handlers[point]...)
	h.mu.Unlock()

	returns := []any{}
	for _, handler := range handlers {
		value, ok := invokeHookHandler(point, handler, ctx)
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
			return
		}
	}
}

func (h *Hooks) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers = map[string][]HookHandler{}
}

func invokeHookHandler(point string, handler HookHandler, ctx map[string]any) (value any, ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if session, typed := ctx["session"].(*Session); typed && session.Observation != nil {
				session.Observation.Emit("hook_handler_failed", map[string]any{
					"point":   point,
					"handler": "harnas.HookHandler",
					"error":   recovered,
				})
			}
			value = nil
			ok = false
		}
	}()
	return handler(ctx), true
}

func funcEqualHook(left, right HookHandler) bool {
	return funcValuePointer(left) == funcValuePointer(right)
}
