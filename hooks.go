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
		if value := handler(ctx); value != nil {
			returns = append(returns, value)
		}
	}
	return returns
}
