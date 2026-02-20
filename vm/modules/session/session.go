package session

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func init() {
	vm.Register(core.TxSessionOpen, handleSessionOpen)
	vm.Register(core.TxSessionResult, handleSessionResult)
}

func handleSessionOpen(ctx *vm.Context, payload json.RawMessage) error {
	var p core.SessionOpenPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode session_open payload: %w", err)
	}
	if p.SessionID == "" {
		return errors.New("session_id required")
	}
	if len(p.Players) == 0 {
		return errors.New("at least one player required")
	}

	// Check session doesn't already exist; distinguish DB errors from not-found.
	if _, err := ctx.State.GetSession(p.SessionID); err == nil {
		return fmt.Errorf("session %q already exists", p.SessionID)
	} else if !errors.Is(err, core.ErrNotFound) {
		return fmt.Errorf("checking session %q: %w", p.SessionID, err)
	}

	// Lock stakes from each player
	if p.Stakes > 0 {
		for _, player := range p.Players {
			acc, err := ctx.State.GetAccount(player)
			if err != nil {
				return fmt.Errorf("player %q account: %w", player, err)
			}
			if acc.Balance < p.Stakes {
				return fmt.Errorf("player %q insufficient balance for stakes: have %d need %d",
					player, acc.Balance, p.Stakes)
			}
			acc.Balance -= p.Stakes
			if err := ctx.State.SetAccount(acc); err != nil {
				return err
			}
		}
	}

	sess := &core.Session{
		ID:        p.SessionID,
		GameID:    p.GameID,
		Players:   p.Players,
		Stakes:    p.Stakes,
		Status:    "open",
		Outcome:   map[string]uint64{},
		CreatedAt: ctx.Block.Header.Timestamp,
	}
	if err := ctx.State.SetSession(sess); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventSessionOpen,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"session_id": p.SessionID, "game_id": p.GameID, "players": p.Players},
		})
	}
	return nil
}

func handleSessionResult(ctx *vm.Context, payload json.RawMessage) error {
	var p core.SessionResultPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode session_result payload: %w", err)
	}

	sess, err := ctx.State.GetSession(p.SessionID)
	if err != nil {
		return fmt.Errorf("session %q not found: %w", p.SessionID, err)
	}
	if sess.Status != "open" {
		return fmt.Errorf("session %q already closed", p.SessionID)
	}

	// Validate total rewards do not exceed total locked stakes (no token creation).
	// Each addition is checked for overflow before proceeding.
	totalStakes := sess.Stakes * uint64(len(sess.Players))
	var totalRewards uint64
	for _, reward := range p.Outcome {
		if reward > totalStakes-totalRewards {
			return fmt.Errorf("rewards exceed total stakes %d", totalStakes)
		}
		totalRewards += reward
	}

	// Distribute rewards
	for pubkey, reward := range p.Outcome {
		acc, err := ctx.State.GetAccount(pubkey)
		if err != nil {
			return fmt.Errorf("outcome account %q: %w", pubkey, err)
		}
		acc.Balance += reward
		if err := ctx.State.SetAccount(acc); err != nil {
			return err
		}
	}

	sess.Status = "closed"
	sess.Outcome = p.Outcome
	sess.ClosedAt = ctx.Block.Header.Timestamp
	if err := ctx.State.SetSession(sess); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventSessionClose,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"session_id": p.SessionID},
		})
	}
	return nil
}
