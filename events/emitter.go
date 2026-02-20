package events

import (
	"log"
	"sync"
)

// EventType labels what happened.
type EventType string

const (
	EventBlockCommit   EventType = "block_commit"
	EventTxExecuted    EventType = "tx_executed"
	EventTokenTransfer EventType = "token_transfer"
	EventAssetMinted   EventType = "asset_minted"
	EventAssetBurned   EventType = "asset_burned"
	EventAssetTransfer EventType = "asset_transfer"
	EventTemplateReg   EventType = "template_registered"
	EventSessionOpen   EventType = "session_open"
	EventSessionClose  EventType = "session_close"
	EventMarketList    EventType = "market_list"
	EventMarketBuy     EventType = "market_buy"
)

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
					log.Printf("[events] handler panicked for %s: %v", ev.Type, r)
				}
			}()
			h(ev)
		}()
	}
}
