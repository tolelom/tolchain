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

const maxSessionPlayers = 10

func handleSessionOpen(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.SessionOpenPayload](payload, core.TxSessionOpen)
	if err != nil {
		return err
	}
	if err := validatePlayers(p); err != nil {
		return err
	}
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}
	if _, err := ctx.State.GetSession(p.SessionID); err == nil {
		return fmt.Errorf("session %q already exists: %w", p.SessionID, core.ErrAlreadyExists)
	} else if !errors.Is(err, core.ErrNotFound) {
		return fmt.Errorf("checking session %q: %w", p.SessionID, err)
	}
	if err := verifyConsents(ctx, p); err != nil {
		return err
	}
	if err := lockStakes(ctx.State, p.Players, p.Stakes); err != nil {
		return err
	}

	sess := &core.Session{
		ID:        p.SessionID,
		GameID:    p.GameID,
		Creator:   ctx.EffectiveSender,
		Players:   p.Players,
		Stakes:    p.Stakes,
		Status:    core.SessionStatusOpen,
		Outcome:   map[string]uint64{},
		CreatedAt: ctx.Block.Header.Timestamp,
	}
	if err := ctx.State.SetSession(sess); err != nil {
		return err
	}

	ctx.Emit(events.EventSessionOpen, events.SessionOpenData{SessionID: p.SessionID, GameID: p.GameID, Players: p.Players})
	return nil
}

func validatePlayers(p *core.SessionOpenPayload) error {
	if p.SessionID == "" {
		return errors.New("session_id required")
	}
	if len(p.Players) == 0 {
		return errors.New("at least one player required")
	}
	if len(p.Players) > maxSessionPlayers {
		return fmt.Errorf("too many players: max %d, got %d", maxSessionPlayers, len(p.Players))
	}
	seen := make(map[string]bool, len(p.Players))
	for _, player := range p.Players {
		if seen[player] {
			return fmt.Errorf("duplicate player: %s", player)
		}
		seen[player] = true
	}
	return nil
}

// verifyConsents requires every player other than the sender to have signed
// core.SessionConsentMessage. The chain ID comes from the block header, which
// the executor has already matched against the tx.
func verifyConsents(ctx *vm.Context, p *core.SessionOpenPayload) error {
	consentMsg := core.SessionConsentMessage(ctx.Block.Header.ChainID, p.SessionID, p.GameID, p.Stakes)
	for _, player := range p.Players {
		if player == ctx.EffectiveSender {
			continue
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
	return nil
}

func lockStakes(state core.State, players []string, stakes uint64) error {
	if stakes == 0 {
		return nil
	}
	for _, player := range players {
		acc, err := state.GetAccount(player)
		if err != nil {
			return fmt.Errorf("player %q account: %w", player, err)
		}
		if acc.Balance < stakes {
			return fmt.Errorf("player %q insufficient balance for stakes: have %d need %d: %w",
				player, acc.Balance, stakes, core.ErrInsufficientBalance)
		}
		acc.Balance -= stakes
		if err := state.SetAccount(acc); err != nil {
			return err
		}
	}
	return nil
}

func handleSessionResult(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.SessionResultPayload](payload, core.TxSessionResult)
	if err != nil {
		return err
	}
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}

	sess, err := ctx.State.GetSession(p.SessionID)
	if err != nil {
		return fmt.Errorf("session %q not found: %w", p.SessionID, err)
	}
	if sess.Status != core.SessionStatusOpen {
		return fmt.Errorf("session %q already closed", p.SessionID)
	}
	if ctx.EffectiveSender != sess.Creator {
		return fmt.Errorf("only the session creator can submit results: %w", core.ErrUnauthorized)
	}
	if err := validateOutcome(sess, p.Outcome); err != nil {
		return err
	}

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

	sess.Status = core.SessionStatusClosed
	sess.Outcome = p.Outcome
	sess.ClosedAt = ctx.Block.Header.Timestamp
	if err := ctx.State.SetSession(sess); err != nil {
		return err
	}

	ctx.Emit(events.EventSessionClose, events.SessionCloseData{SessionID: p.SessionID})
	return nil
}

// validateOutcome requires rewards to go only to players and to add up to
// exactly the locked stakes, so a result neither mints nor burns tokens.
func validateOutcome(sess *core.Session, outcome map[string]uint64) error {
	playerSet := make(map[string]bool, len(sess.Players))
	for _, player := range sess.Players {
		playerSet[player] = true
	}
	for pubkey := range outcome {
		if !playerSet[pubkey] {
			return fmt.Errorf("outcome recipient %q is not a session player", pubkey)
		}
	}

	nPlayers := uint64(len(sess.Players))
	if sess.Stakes > 0 && nPlayers > math.MaxUint64/sess.Stakes {
		return fmt.Errorf("total stakes overflow")
	}
	totalStakes := sess.Stakes * nPlayers
	var totalRewards uint64
	for _, reward := range outcome {
		if reward > totalStakes-totalRewards {
			return fmt.Errorf("rewards exceed total stakes %d", totalStakes)
		}
		totalRewards += reward
	}
	if totalRewards != totalStakes {
		return fmt.Errorf("rewards (%d) must equal total stakes (%d); undistributed tokens would be lost", totalRewards, totalStakes)
	}
	return nil
}

func handleSessionCancel(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.SessionCancelPayload](payload, core.TxSessionCancel)
	if err != nil {
		return err
	}

	sess, err := ctx.State.GetSession(p.SessionID)
	if err != nil {
		return fmt.Errorf("session %q not found: %w", p.SessionID, err)
	}
	if sess.Status != core.SessionStatusOpen {
		return fmt.Errorf("session %q is not open (status=%s)", p.SessionID, sess.Status)
	}
	if ctx.EffectiveSender != sess.Creator {
		return fmt.Errorf("only the session creator can cancel it: %w", core.ErrUnauthorized)
	}
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}
	if err := refundStakes(ctx.State, sess); err != nil {
		return err
	}

	sess.Status = core.SessionStatusCancelled
	sess.ClosedAt = ctx.Block.Header.Timestamp
	if err := ctx.State.SetSession(sess); err != nil {
		return err
	}

	ctx.Emit(events.EventSessionCancel, events.SessionCancelData{SessionID: p.SessionID})
	return nil
}

func refundStakes(state core.State, sess *core.Session) error {
	if sess.Stakes == 0 {
		return nil
	}
	for _, player := range sess.Players {
		acc, err := state.GetAccount(player)
		if err != nil {
			return fmt.Errorf("player %q account: %w", player, err)
		}
		newBal, err := vm.SafeAdd(acc.Balance, sess.Stakes)
		if err != nil {
			return fmt.Errorf("player %q balance overflow on refund: %w", player, err)
		}
		acc.Balance = newBal
		if err := state.SetAccount(acc); err != nil {
			return err
		}
	}
	return nil
}
