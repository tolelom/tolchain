package random

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func init() {
	vm.Register(core.TxRandomCommit, handleRandomCommit)
	vm.Register(core.TxRandomReveal, handleRandomReveal)
}

func handleRandomCommit(ctx *vm.Context, payload json.RawMessage) error {
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}

	var p core.RandomCommitPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode random_commit payload: %w", err)
	}
	if p.CommitID == "" || p.CommitHash == "" {
		return errors.New("commit_id and commit_hash required")
	}
	if _, err := hex.DecodeString(p.CommitHash); err != nil {
		return fmt.Errorf("commit_hash must be hex: %w", err)
	}

	// Check for duplicate.
	_, err := ctx.State.GetRandomCommitment(p.CommitID)
	if err == nil {
		return fmt.Errorf("commitment %q already exists: %w", p.CommitID, core.ErrAlreadyExists)
	}
	if !errors.Is(err, core.ErrNotFound) {
		return err
	}

	rc := &core.RandomCommitment{
		ID:          p.CommitID,
		Committer:   ctx.Tx.From,
		CommitHash:  p.CommitHash,
		BlockHeight: ctx.Block.Header.Height,
	}
	if err := ctx.State.SetRandomCommitment(rc); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventRandomCommit,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"commit_id": p.CommitID, "committer": ctx.Tx.From},
		})
	}
	return nil
}

func handleRandomReveal(ctx *vm.Context, payload json.RawMessage) error {
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}

	var p core.RandomRevealPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode random_reveal payload: %w", err)
	}
	if p.CommitID == "" || p.Secret == "" {
		return errors.New("commit_id and secret required")
	}

	rc, err := ctx.State.GetRandomCommitment(p.CommitID)
	if err != nil {
		return fmt.Errorf("commitment %q not found: %w", p.CommitID, err)
	}
	if rc.Revealed {
		return errors.New("already revealed")
	}
	if rc.Committer != ctx.Tx.From {
		return errors.New("only the committer can reveal")
	}

	// Verify: hash(secret) must equal commit_hash.
	secretHash := crypto.Hash([]byte(p.Secret))
	if secretHash != rc.CommitHash {
		return errors.New("secret does not match commitment hash")
	}

	// Compute result: hash(secret + prevBlockHash) for unpredictability.
	result := crypto.Hash([]byte(p.Secret + ctx.Block.Header.PrevHash))

	rc.Revealed = true
	rc.Secret = p.Secret
	rc.Result = result
	if err := ctx.State.SetRandomCommitment(rc); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventRandomReveal,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"commit_id": p.CommitID, "result": result},
		})
	}
	return nil
}
