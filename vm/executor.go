package vm

import (
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
)

// Executor applies transactions to the state through the global Handler
// registry. Each tx runs under its own snapshot and is rolled back on failure.
type Executor struct {
	mu             sync.Mutex
	state          core.State
	emitter        events.EventEmitter
	operators      map[string]bool
	maxTotalSupply uint64
	blockHash      func(height int64) (string, error)
}

func NewExecutor(state core.State, emitter events.EventEmitter) *Executor {
	return &Executor{state: state, emitter: emitter}
}

// SetEmitter swaps the emitter, e.g. for a Buffer while a block executes.
func (e *Executor) SetEmitter(em events.EventEmitter) {
	e.mu.Lock()
	e.emitter = em
	e.mu.Unlock()
}

// SetMaxTotalSupply caps tokens minted by rewards; 0 means unlimited.
func (e *Executor) SetMaxTotalSupply(cap uint64) {
	e.mu.Lock()
	e.maxTotalSupply = cap
	e.mu.Unlock()
}

func (e *Executor) SetBlockHashes(fn func(height int64) (string, error)) {
	e.mu.Lock()
	e.blockHash = fn
	e.mu.Unlock()
}

func (e *Executor) SetOperators(ops []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(ops) == 0 {
		e.operators = nil
		return
	}
	m := make(map[string]bool, len(ops))
	for _, o := range ops {
		m[o] = true
	}
	e.operators = m
}

// ExecuteBlock applies a received block; any failing tx rejects the block.
func (e *Executor) ExecuteBlock(block *core.Block) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tx := range block.Transactions {
		if err := e.executeTxLocked(block, tx); err != nil {
			return fmt.Errorf("tx %s failed: %w", tx.ID, err)
		}
	}
	return nil
}

func (e *Executor) ExecuteTx(block *core.Block, tx *core.Transaction) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.executeTxLocked(block, tx)
}

func (e *Executor) executeTxLocked(block *core.Block, tx *core.Transaction) error {
	// The signature already binds the tx to its chain, but a malicious
	// validator could still include a foreign chain's validly signed tx.
	if tx.ChainID != block.Header.ChainID {
		return fmt.Errorf("tx chain_id %q does not match block chain_id %q", tx.ChainID, block.Header.ChainID)
	}
	if err := tx.Verify(); err != nil {
		return fmt.Errorf("signature: %w", err)
	}

	snapID, err := e.state.Snapshot()
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	txEvents := events.NewBuffer()
	if err := e.applyTx(block, tx, txEvents); err != nil {
		if revertErr := e.state.RevertToSnapshot(snapID); revertErr != nil {
			return fmt.Errorf("revert snapshot after tx failure: %w (revert: %v)", err, revertErr)
		}
		e.emitTxExecuted(block, tx, err)
		return err
	}
	if e.emitter != nil {
		txEvents.Flush(e.emitter)
	}
	e.emitTxExecuted(block, tx, nil)
	return nil
}

func (e *Executor) emitTxExecuted(block *core.Block, tx *core.Transaction, txErr error) {
	if e.emitter == nil {
		return
	}
	data := events.TxExecutedData{Type: string(tx.Type), From: tx.From, Success: txErr == nil}
	if txErr != nil {
		data.Error = txErr.Error()
	}
	e.emitter.Emit(events.Event{
		Type:        events.EventTxExecuted,
		TxID:        tx.ID,
		BlockHeight: block.Header.Height,
		Data:        data.Map(),
	})
}

func (e *Executor) applyTx(block *core.Block, tx *core.Transaction, emitter events.EventEmitter) error {
	effectiveSender := tx.From
	if tx.OnBehalfOf != "" {
		if err := e.useDelegation(block, tx); err != nil {
			return fmt.Errorf("delegation: %w", err)
		}
		effectiveSender = tx.OnBehalfOf
	}
	if err := e.chargeFeeAndNonce(block, tx, effectiveSender); err != nil {
		return err
	}
	ctx := &Context{
		State:           e.state,
		Block:           block,
		Tx:              tx,
		Emitter:         emitter,
		Operators:       e.operators,
		MaxTotalSupply:  e.maxTotalSupply,
		EffectiveSender: effectiveSender,
		BlockHash:       e.blockHash,
	}
	return runHandler(tx, ctx)
}

// useDelegation checks that tx.From may act for tx.OnBehalfOf and counts one
// use of the grant. Expiry is judged by the block timestamp, not the wall
// clock, so replaying nodes reach the same verdict.
func (e *Executor) useDelegation(block *core.Block, tx *core.Transaction) error {
	if tx.Type == core.TxGrantDelegation || tx.Type == core.TxRevokeDelegation {
		return fmt.Errorf("cannot execute %s via delegation", tx.Type)
	}
	grant, err := e.state.GetDelegation(tx.OnBehalfOf, tx.From)
	if err != nil {
		return fmt.Errorf("grant not found: %w", err)
	}
	blockTimeSec := block.Header.Timestamp / int64(time.Second)
	if grant.ExpiresAt > 0 && blockTimeSec > grant.ExpiresAt {
		return fmt.Errorf("delegation expired")
	}
	if grant.MaxUses > 0 && grant.UsedCount >= grant.MaxUses {
		return fmt.Errorf("delegation max uses exceeded")
	}
	if !slices.ContainsFunc(grant.AllowTypes, func(t string) bool { return core.TxType(t) == tx.Type }) {
		return fmt.Errorf("tx type %s not allowed by delegation", tx.Type)
	}
	grant.UsedCount++
	if err := e.state.SetDelegation(grant); err != nil {
		return fmt.Errorf("update delegation grant: %w", err)
	}
	return nil
}

// chargeFeeAndNonce consumes the sender's nonce and pays the fee to the block
// proposer. Without a proposer the fee is burned.
func (e *Executor) chargeFeeAndNonce(block *core.Block, tx *core.Transaction, sender string) error {
	acc, err := e.state.GetAccount(sender)
	if err != nil {
		return fmt.Errorf("get account: %w", err)
	}
	if acc.Nonce != tx.Nonce {
		return fmt.Errorf("invalid nonce: expected %d got %d: %w", acc.Nonce, tx.Nonce, core.ErrInvalidNonce)
	}
	if acc.Balance < tx.Fee {
		return fmt.Errorf("insufficient balance for fee: have %d need %d: %w", acc.Balance, tx.Fee, core.ErrInsufficientBalance)
	}
	if acc.Nonce == math.MaxUint64 {
		return fmt.Errorf("nonce overflow for account %s: %w", sender, core.ErrNonceOverflow)
	}
	acc.Balance -= tx.Fee
	acc.Nonce++

	proposer := block.Header.Proposer
	switch {
	case tx.Fee == 0 || proposer == "":
		return e.state.SetAccount(acc)
	case sender == proposer:
		// Same account: settle on this struct, or a second load-and-save
		// would overwrite the nonce increment.
		acc.Balance += tx.Fee
		return e.state.SetAccount(acc)
	}

	if err := e.state.SetAccount(acc); err != nil {
		return err
	}
	proposerAcc, err := e.state.GetAccount(proposer)
	if err != nil {
		return fmt.Errorf("get proposer account: %w", err)
	}
	newBal, err := SafeAdd(proposerAcc.Balance, tx.Fee)
	if err != nil {
		return fmt.Errorf("proposer balance overflow: %w", core.ErrBalanceOverflow)
	}
	proposerAcc.Balance = newBal
	if err := e.state.SetAccount(proposerAcc); err != nil {
		return fmt.Errorf("set proposer account: %w", err)
	}
	return nil
}

// runHandler turns a handler panic into a tx failure so the snapshot is still
// reverted and the executor lock released.
func runHandler(tx *core.Transaction, ctx *Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic for %s: %v", tx.Type, r)
		}
	}()
	return globalRegistry.Execute(tx.Type, ctx, tx.Payload)
}
