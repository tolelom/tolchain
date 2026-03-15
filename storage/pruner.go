package storage

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

const (
	metaPrunedHeight = "metadata:pruned_height"
)

// Pruner removes old blocks and their height index entries
// to limit disk usage on long-running nodes. It preserves the
// genesis block (height 0) and the most recent keepBlocks blocks.
type Pruner struct {
	db         DB
	keepBlocks int64
}

// NewPruner creates a pruner. keepBlocks is the number of recent
// blocks to retain. If keepBlocks <= 0, Prune() is a no-op.
func NewPruner(db DB, keepBlocks int64) *Pruner {
	return &Pruner{db: db, keepBlocks: keepBlocks}
}

// Prune deletes blocks below currentHeight - keepBlocks.
// It skips the genesis block (height 0) and is idempotent.
//
// NOTE: Transaction index entries (idx:tx:*) for pruned blocks are not
// cleaned up here because deserializing each block to extract tx IDs
// would negate the performance benefit of pruning. These orphan index
// entries are harmless and will be cleaned up during a full reindex.
func (p *Pruner) Prune(currentHeight int64) error {
	if p.keepBlocks <= 0 || currentHeight <= p.keepBlocks {
		return nil
	}

	pruneBelow := currentHeight - p.keepBlocks

	// Resume from last pruned height to avoid re-scanning.
	startHeight := p.lastPrunedHeight() + 1

	// Genesis block (height 0) is never pruned.
	if startHeight < 1 {
		startHeight = 1
	}
	if startHeight >= pruneBelow {
		return nil
	}

	batch := p.db.NewBatch()
	pruned := 0

	for h := startHeight; h < pruneBelow; h++ {
		heightKey := fmt.Sprintf("height:%d", h)

		hash, err := p.db.Get([]byte(heightKey))
		if err != nil {
			// Already pruned or missing — skip.
			continue
		}

		batch.Delete([]byte("block:" + string(hash)))
		batch.Delete([]byte(heightKey))
		pruned++

		// Flush in chunks to avoid large batch memory.
		if pruned%1000 == 0 {
			batch.Set([]byte(metaPrunedHeight), []byte(strconv.FormatInt(h, 10)))
			if err := batch.Write(); err != nil {
				return fmt.Errorf("prune batch write: %w", err)
			}
			batch.Reset()
		}
	}

	if pruned%1000 != 0 {
		batch.Set([]byte(metaPrunedHeight), []byte(strconv.FormatInt(pruneBelow-1, 10)))
		if err := batch.Write(); err != nil {
			return fmt.Errorf("prune batch write: %w", err)
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

// lastPrunedHeight returns the last pruned height, or 0 if never pruned.
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
