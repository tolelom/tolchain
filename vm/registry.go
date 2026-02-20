package vm

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tolelom/tolchain/core"
)

// Handler is the function signature every transaction module must implement.
type Handler func(ctx *Context, payload json.RawMessage) error

// Registry maps TxTypes to Handlers. Thread-safe for concurrent registration.
type Registry struct {
	mu       sync.RWMutex
	handlers map[core.TxType]Handler
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[core.TxType]Handler)}
}

// Register associates typ with h. Panics on duplicate registration.
func (r *Registry) Register(typ core.TxType, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[typ]; exists {
		panic(fmt.Sprintf("vm: handler already registered for TxType %q", typ))
	}
	r.handlers[typ] = h
}

// Execute dispatches payload to the handler registered for typ.
func (r *Registry) Execute(typ core.TxType, ctx *Context, payload json.RawMessage) error {
	r.mu.RLock()
	h, ok := r.handlers[typ]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("vm: no handler registered for TxType %q", typ)
	}
	return h(ctx, payload)
}

// globalRegistry is the package-level singleton that modules register into.
var globalRegistry = NewRegistry()

// Register adds a handler to the global registry.
// Module init() functions call this to self-register.
func Register(typ core.TxType, h Handler) {
	globalRegistry.Register(typ, h)
}
