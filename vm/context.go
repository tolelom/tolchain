package vm

import (
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
)

// Context is what a Handler sees of the chain while executing one tx.
type Context struct {
	State     core.State
	Block     *core.Block
	Tx        *core.Transaction
	Emitter   events.EventEmitter // nil → events are dropped
	Operators map[string]bool     // empty → every sender is an operator
	// MaxTotalSupply caps tokens minted by rewards; 0 → unlimited.
	MaxTotalSupply uint64
	// EffectiveSender is the account the tx acts for: Tx.OnBehalfOf when
	// delegated, otherwise Tx.From.
	EffectiveSender string
	// BlockHash looks up a committed block's hash; nil when not configured.
	BlockHash func(height int64) (string, error)
}

// EventData is implemented by the generated <Event>Data payload types.
type EventData interface {
	Map() map[string]any
}

// Emit publishes an event for the current tx.
func (c *Context) Emit(typ events.EventType, data EventData) {
	if c.Emitter == nil {
		return
	}
	c.Emitter.Emit(events.Event{
		Type:        typ,
		TxID:        c.Tx.ID,
		BlockHeight: c.Block.Header.Height,
		Data:        data.Map(),
	})
}
