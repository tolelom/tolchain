package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func init() {
	vm.Register(core.TxSessionOpen, handleSessionOpen)
	vm.Register(core.TxSessionResult, handleSessionResult)
	vm.Register(core.TxSessionCancel, handleSessionCancel)
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

	// Operator restriction: only authorised operators can open sessions.
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}

	// Check session doesn't already exist; distinguish DB errors from not-found.
	if _, err := ctx.State.GetSession(p.SessionID); err == nil {
		return fmt.Errorf("session %q already exists: %w", p.SessionID, core.ErrAlreadyExists)
	} else if !errors.Is(err, core.ErrNotFound) {
		return fmt.Errorf("checking session %q: %w", p.SessionID, err)
	}

	// Verify that every player (except tx sender) has signed
	// "session:<sessionID>" to prove consent — regardless of stakes.
	consentMsg := []byte("session:" + p.SessionID)
	for _, player := range p.Players {
		if player == ctx.Tx.From {
			continue // the sender implicitly consents by submitting the tx
		}
		sig, ok := p.Signatures[player]
		if !ok || sig == "" {
			return fmt.Errorf("missing consent signature from player %q", player)
		}
		if err := vm.ValidatePubKey(player, fmt.Sprintf("player pubkey %q", player)); err != nil {
			return err
		}
		pub, _ := crypto.PubKeyFromHex(player)
		if err := crypto.Verify(pub, consentMsg, sig); err != nil {
			return fmt.Errorf("invalid consent signature from player %q: %w", player, err)
		}
	}

	// Lock stakes from each player
	if p.Stakes > 0 {
		for _, player := range p.Players {
			acc, err := ctx.State.GetAccount(player)
			if err != nil {
				return fmt.Errorf("player %q account: %w", player, err)
			}
			if acc.Balance < p.Stakes {
				return fmt.Errorf("player %q insufficient balance for stakes: have %d need %d: %w",
					player, acc.Balance, p.Stakes, core.ErrInsufficientBalance)
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
		Creator:   ctx.Tx.From, // opener is the only one who can submit the result
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
	// Only the session creator (game server / oracle that opened it) can submit results.
	if ctx.Tx.From != sess.Creator {
		return fmt.Errorf("only the session creator can submit results: %w", core.ErrUnauthorized)
	}

	// Build a set of valid players to reject payouts to arbitrary addresses.
	playerSet := make(map[string]bool, len(sess.Players))
	for _, player := range sess.Players {
		playerSet[player] = true
	}
	for pubkey := range p.Outcome {
		if !playerSet[pubkey] {
			return fmt.Errorf("outcome recipient %q is not a session player", pubkey)
		}
	}

	// Validate total rewards do not exceed total locked stakes (no token creation).
	// Each addition is checked for overflow before proceeding.
	nPlayers := uint64(len(sess.Players))
	if sess.Stakes > 0 && nPlayers > math.MaxUint64/sess.Stakes {
		return fmt.Errorf("total stakes overflow")
	}
	totalStakes := sess.Stakes * nPlayers
	var totalRewards uint64
	for _, reward := range p.Outcome {
		if reward > totalStakes-totalRewards {
			return fmt.Errorf("rewards exceed total stakes %d", totalStakes)
		}
		totalRewards += reward
	}
	// (E) Require all staked tokens to be distributed — prevents accidental loss.
	if totalRewards != totalStakes {
		return fmt.Errorf("rewards (%d) must equal total stakes (%d); undistributed tokens would be lost", totalRewards, totalStakes)
	}

	// Distribute rewards
	for pubkey, reward := range p.Outcome {
		acc, err := ctx.State.GetAccount(pubkey)
		if err != nil {
			return fmt.Errorf("outcome account %q: %w", pubkey, err)
		}
		newBal, err := vm.SafeAdd(acc.Balance, reward)
		if err != nil {
			return fmt.Errorf("reward overflow for player %q: %w", pubkey, err)
		}
		acc.Balance = newBal
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

func handleSessionCancel(ctx *vm.Context, payload json.RawMessage) error {
	var p core.SessionCancelPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode session_cancel payload: %w", err)
	}

	sess, err := ctx.State.GetSession(p.SessionID)
	if err != nil {
		return fmt.Errorf("session %q not found: %w", p.SessionID, err)
	}
	if sess.Status != "open" {
		return fmt.Errorf("session %q is not open (status=%s)", p.SessionID, sess.Status)
	}
	if ctx.Tx.From != sess.Creator {
		return fmt.Errorf("only the session creator can cancel it: %w", core.ErrUnauthorized)
	}

	// Operator restriction.
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}

	// Refund stakes to all players.
	if sess.Stakes > 0 {
		for _, player := range sess.Players {
			acc, err := ctx.State.GetAccount(player)
			if err != nil {
				return fmt.Errorf("player %q account: %w", player, err)
			}
			newBal, err := vm.SafeAdd(acc.Balance, sess.Stakes)
			if err != nil {
				return fmt.Errorf("player %q balance overflow on refund: %w", player, err)
			}
			acc.Balance = newBal
			if err := ctx.State.SetAccount(acc); err != nil {
				return err
			}
		}
	}

	sess.Status = "cancelled"
	sess.ClosedAt = ctx.Block.Header.Timestamp
	if err := ctx.State.SetSession(sess); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventSessionCancel,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"session_id": p.SessionID},
		})
	}
	return nil
}
