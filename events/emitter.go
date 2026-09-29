package events

import (
	"log/slog"
	"sync"
)

// EventType과 그 상수, EventTypeValues는 설계(design/tolchain.yaml)에서 생성한다: event_type.gen.go
// 이벤트별 Data의 모양은 chain_event.gen.go의 <이벤트>Data가 정한다.

// Event carries a typed payload emitted after a state change.
type Event struct {
	Type        EventType      `json:"type"`
	TxID        string         `json:"tx_id"`
	BlockHeight int64          `json:"block_height"`
	Data        map[string]any `json:"data"`
}

// Handler is a callback invoked for matching events.
type Handler func(Event)

// Emitter is a simple pub/sub broker. Subscribe before Emit.
type Emitter struct {
	mu       sync.RWMutex
	handlers map[EventType][]Handler
}

// NewEmitter creates an Emitter with no subscribers.
func NewEmitter() *Emitter {
	return &Emitter{handlers: make(map[EventType][]Handler)}
}

// Subscribe registers h to be called whenever typ is emitted.
func (e *Emitter) Subscribe(typ EventType, h Handler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[typ] = append(e.handlers[typ], h)
}

// Emit delivers ev to all subscribers for ev.Type synchronously.
// Each handler is guarded by panic recovery so a misbehaving subscriber
// cannot crash the node or halt block production.
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
