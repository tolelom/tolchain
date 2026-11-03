package vm

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tolelom/tolchain/core"
)

type Handler func(ctx *Context, payload json.RawMessage) error

type Registry struct {
	mu       sync.RWMutex
	handlers map[core.TxType]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[core.TxType]Handler)}
}

// Register panics on a duplicate type: two modules claiming one tx type is a
// build error.
func (r *Registry) Register(typ core.TxType, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[typ]; exists {
		panic(fmt.Sprintf("vm: handler already registered for TxType %q", typ))
	}
	r.handlers[typ] = h
}

func (r *Registry) Execute(typ core.TxType, ctx *Context, payload json.RawMessage) error {
	r.mu.RLock()
	h, ok := r.handlers[typ]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("vm: no handler registered for TxType %q", typ)
	}
	return h(ctx, payload)
}

var globalRegistry = NewRegistry()

// Register is called from module init functions (generated register.gen.go).
func Register(typ core.TxType, h Handler) {
	globalRegistry.Register(typ, h)
}
