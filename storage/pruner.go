package storage

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/tolelom/tolchain/core"
)

const (
	metaPrunedHeight = "metadata:pruned_height"
	pruneBatchSize   = 1000
)

// Pruner deletes old blocks and their height index, always keeping genesis
// and the latest keepBlocks blocks.
type Pruner struct {
	db         DB
	keepBlocks int64
}

// NewPruner returns a pruner; keepBlocks <= 0 disables pruning.
func NewPruner(db DB, keepBlocks int64) *Pruner {
	return &Pruner{db: db, keepBlocks: keepBlocks}
}

// Prune deletes blocks below currentHeight-keepBlocks, resuming after the last
// pruned height.
//
// Tx index entries (idx:tx:*) of pruned blocks are left in place: removing
// them would mean decoding every pruned block.
func (p *Pruner) Prune(currentHeight int64) error {
	if p.keepBlocks <= 0 || currentHeight <= p.keepBlocks {
		return nil
	}
	pruneBelow := currentHeight - p.keepBlocks
	startHeight := max(p.lastPrunedHeight()+1, 1)
	if startHeight >= pruneBelow {
		return nil
	}

	batch := p.db.NewBatch()
	pruned := 0
	for h := startHeight; h < pruneBelow; h++ {
		hash, err := p.db.Get(heightKey(h))
		if errors.Is(err, core.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("prune: read height %d: %w", h, err)
		}
		batch.Delete(blockKey(string(hash)))
		batch.Delete(heightKey(h))
		pruned++
		if pruned%pruneBatchSize == 0 {
			if err := writePruneBatch(batch, h); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	if pruned%pruneBatchSize != 0 {
		if err := writePruneBatch(batch, pruneBelow-1); err != nil {
			return err
		}
	}

	if pruned > 0 {
		slog.Info("blocks pruned",
			"pruned_count", pruned,
			"pruned_up_to", pruneBelow-1,
			"current_height", currentHeight,
		)
	}
	return nil
}

func writePruneBatch(batch Batch, prunedUpTo int64) error {
	batch.Set([]byte(metaPrunedHeight), []byte(strconv.FormatInt(prunedUpTo, 10)))
	if err := batch.Write(); err != nil {
		return fmt.Errorf("prune batch write: %w", err)
	}
	return nil
}

// lastPrunedHeight returns 0 when nothing was pruned yet.
func (p *Pruner) lastPrunedHeight() int64 {
	val, err := p.db.Get([]byte(metaPrunedHeight))
	if err != nil {
		return 0
	}
	h, err := strconv.ParseInt(strings.TrimSpace(string(val)), 10, 64)
	if err != nil {
		return 0
	}
	return h
}
