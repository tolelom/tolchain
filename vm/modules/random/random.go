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

func handleRandomCommit(ctx *vm.Context, payload json.RawMessage) error {
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}
	p, err := vm.DecodePayload[core.RandomCommitPayload](payload, core.TxRandomCommit)
	if err != nil {
		return err
	}
	if p.CommitID == "" || p.CommitHash == "" {
		return errors.New("commit_id and commit_hash required")
	}
	if _, err := hex.DecodeString(p.CommitHash); err != nil {
		return fmt.Errorf("commit_hash must be hex: %w", err)
	}

	_, err = ctx.State.GetRandomCommitment(p.CommitID)
	if err == nil {
		return fmt.Errorf("commitment %q already exists: %w", p.CommitID, core.ErrAlreadyExists)
	}
	if !errors.Is(err, core.ErrNotFound) {
		return err
	}

	rc := &core.RandomCommitment{
		ID:          p.CommitID,
		Committer:   ctx.EffectiveSender,
		CommitHash:  p.CommitHash,
		BlockHeight: ctx.Block.Header.Height,
	}
	if err := ctx.State.SetRandomCommitment(rc); err != nil {
		return err
	}

	ctx.Emit(events.EventRandomCommit, events.RandomCommitData{CommitID: p.CommitID, Committer: ctx.EffectiveSender})
	return nil
}

func handleRandomReveal(ctx *vm.Context, payload json.RawMessage) error {
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}
	p, err := vm.DecodePayload[core.RandomRevealPayload](payload, core.TxRandomReveal)
	if err != nil {
		return err
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
	if rc.Committer != ctx.EffectiveSender {
		return errors.New("only the committer can reveal")
	}

	// 결과는 커밋 뒤에 생기는 목표 블록의 해시에 묶는다. 커밋 시점에는 아무도 모르고,
	// 공개 블록을 골라도 바뀌지 않는다.
	target := rc.BlockHeight + core.RandomTargetDelay
	if ctx.Block.Header.Height <= target {
		return fmt.Errorf("reveal too early: reveal allowed from height %d", target+1)
	}
	if ctx.Block.Header.Height > target+core.RandomRevealWindow {
		return fmt.Errorf("reveal expired: window closed at height %d", target+core.RandomRevealWindow)
	}
	if crypto.Hash([]byte(p.Secret)) != rc.CommitHash {
		return errors.New("secret does not match commitment hash")
	}
	if ctx.BlockHash == nil {
		return errors.New("block hash lookup not configured")
	}
	targetHash, err := ctx.BlockHash(target)
	if err != nil {
		return fmt.Errorf("target block %d: %w", target, err)
	}
	result := crypto.Hash([]byte(p.Secret + targetHash))

	rc.Revealed = true
	rc.Secret = p.Secret
	rc.Result = result
	if err := ctx.State.SetRandomCommitment(rc); err != nil {
		return err
	}

	ctx.Emit(events.EventRandomReveal, events.RandomRevealData{CommitID: p.CommitID, Result: result})
	return nil
}
