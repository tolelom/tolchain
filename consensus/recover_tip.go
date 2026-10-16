package consensus

import (
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
)

func (p *PoA) RecoverTipState() (bool, error) {
	tip := p.bc.Tip()
	if tip == nil || tip.Header.Height == 0 || tip.Header.StateRoot == "" {
		return false, nil
	}
	p.state.BlockLock()
	defer p.state.BlockUnlock()

	root, err := p.state.ComputeRoot()
	if err != nil {
		return false, fmt.Errorf("compute state root: %w", err)
	}
	if root == tip.Header.StateRoot {
		return false, nil
	}

	snapID, err := p.state.Snapshot()
	if err != nil {
		return false, err
	}
	buf := events.NewBuffer()
	p.exec.SetEmitter(buf)
	err = p.replayTip(tip)
	p.exec.SetEmitter(p.emitter)
	if err != nil {
		if revertErr := p.state.RevertToSnapshot(snapID); revertErr != nil {
			return false, fmt.Errorf("%w (revert: %v)", err, revertErr)
		}
		return false, err
	}
	if err := p.state.Commit(); err != nil {
		return false, fmt.Errorf("commit replayed tip state: %w", err)
	}
	buf.Flush(p.emitter)
	return true, nil
}

func (p *PoA) replayTip(tip *core.Block) error {
	if err := p.exec.ExecuteBlock(tip); err != nil {
		return fmt.Errorf("state does not match tip %d and replaying it failed (state is more than one block behind?): %w", tip.Header.Height, err)
	}
	root, err := p.state.ComputeRoot()
	if err != nil {
		return fmt.Errorf("compute state root after replay: %w", err)
	}
	if root != tip.Header.StateRoot {
		return fmt.Errorf("state does not match tip %d even after replaying it: computed %s, want %s", tip.Header.Height, root, tip.Header.StateRoot)
	}
	return nil
}
