package events

import (
	"log/slog"
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
	EventMarketCancel  EventType = "market_cancel"
	EventSessionCancel   EventType = "session_cancel"
	EventItemEquipped    EventType = "item_equipped"
	EventItemUnequipped  EventType = "item_unequipped"
	EventRewardGranted   EventType = "reward_granted"
	EventRandomCommit    EventType = "random_commit"
	EventRandomReveal    EventType = "random_reveal"
	EventDelegationGranted EventType = "delegation_granted"
	EventDelegationRevoked EventType = "delegation_revoked"
)

// AllEventTypes returns every event type defined by this package. Keep this
// list in sync with the constants above; consumers that need to subscribe to
// everything (e.g. the SSE broker) must use this instead of their own list.
func AllEventTypes() []EventType {
	return []EventType{
		EventBlockCommit,
		EventTxExecuted,
		EventTokenTransfer,
		EventAssetMinted,
		EventAssetBurned,
		EventAssetTransfer,
		EventTemplateReg,
		EventSessionOpen,
		EventSessionClose,
		EventMarketList,
		EventMarketBuy,
		EventMarketCancel,
		EventSessionCancel,
		EventItemEquipped,
		EventItemUnequipped,
		EventRewardGranted,
		EventRandomCommit,
		EventRandomReveal,
		EventDelegationGranted,
		EventDelegationRevoked,
	}
}

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
