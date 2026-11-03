package events

import (
	"log/slog"
	"sync"
)

// EventType, its values and each event's Data shape (<Event>Data) are
// generated from design/tolchain.yaml.

// Event is serialized as-is to SSE clients.
type Event struct {
	Type        EventType      `json:"type"`
	TxID        string         `json:"tx_id"`
	BlockHeight int64          `json:"block_height"`
	Data        map[string]any `json:"data"`
}

type Handler func(Event)

// Emitter delivers events synchronously to subscribers of their type.
type Emitter struct {
	mu       sync.RWMutex
	handlers map[EventType][]Handler
}

func NewEmitter() *Emitter {
	return &Emitter{handlers: make(map[EventType][]Handler)}
}

func (e *Emitter) Subscribe(typ EventType, h Handler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[typ] = append(e.handlers[typ], h)
}

// Emit recovers a subscriber's panic so it cannot take down block production,
// which emits on its own goroutine.
func (e *Emitter) Emit(ev Event) {
	e.mu.RLock()
	handlers := e.handlers[ev.Type]
	e.mu.RUnlock()
	for _, h := range handlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("event handler panicked", "type", ev.Type, "panic", r)
				}
			}()
			h(ev)
		}()
	}
}
