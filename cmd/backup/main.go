// Command backup exports the current TOL Chain state from LevelDB to a JSON file.
// The database is opened in read-only mode so it can be run while the node is stopped
// (or against a snapshot copy) without risk of corruption.
//
// Usage:
//
//	go run ./cmd/backup -data-dir ./data/chain -output backup.json
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// stateEntry is a single key-value pair in the exported state.
type stateEntry struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// backup is the top-level export format.
type backup struct {
	BlockHeight int64        `json:"block_height"`
	BlockHash   string       `json:"block_hash"`
	ChainTip    string       `json:"chain_tip"`
	State       []stateEntry `json:"state"`
	Blocks      int          `json:"blocks_count"`
	Indexes     int          `json:"indexes_count"`
}

// statePrefixes are the key prefixes used by StateDB for world state.
var statePrefixes = []string{
	"acct:",
	"asset:",
	"tmpl:",
	"sess:",
	"list:",
	"inv:",
	"rcom:",
}

func main() {
	dataDir := flag.String("data-dir", "./data/chain", "LevelDB data directory")
	output := flag.String("output", "backup.json", "Output file path")
	flag.Parse()

	db, err := leveldb.OpenFile(*dataDir, &opt.Options{ReadOnly: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	b := backup{}

	// Read chain tip.
	if tipVal, err := db.Get([]byte("chain:tip"), nil); err == nil {
		b.ChainTip = string(tipVal)
	}

	// If we have a tip, read the tip block to get height.
	if b.ChainTip != "" {
		if blockData, err := db.Get([]byte("block:"+b.ChainTip), nil); err == nil {
			var block struct {
				Hash   string `json:"hash"`
				Header struct {
					Height int64 `json:"height"`
				} `json:"header"`
			}
			if json.Unmarshal(blockData, &block) == nil {
				b.BlockHeight = block.Header.Height
				b.BlockHash = block.Hash
			}
		}
	}

	// Export all state entries.
	for _, prefix := range statePrefixes {
		iter := db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
		for iter.Next() {
			key := string(iter.Key())
			val := make([]byte, len(iter.Value()))
			copy(val, iter.Value())

			// Try to parse as JSON for clean output; fall back to hex.
			var entry stateEntry
			entry.Key = key
			if json.Valid(val) {
				entry.Value = json.RawMessage(val)
			} else {
				hexStr := fmt.Sprintf("%q", hex.EncodeToString(val))
				entry.Value = json.RawMessage(hexStr)
			}
			b.State = append(b.State, entry)
		}
		iter.Release()
		if err := iter.Error(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: iterator error for prefix %q: %v\n", prefix, err)
		}
	}

	// Count blocks and index entries.
	countPrefix := func(prefix string) int {
		count := 0
		iter := db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
		for iter.Next() {
			count++
		}
		iter.Release()
		return count
	}

	b.Blocks = countPrefix("block:")
	// Count all index prefixes (idx:, txr:, etc.)
	indexPrefixes := []string{"idx:", "txr:"}
	for _, p := range indexPrefixes {
		b.Indexes += countPrefix(p)
	}

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal backup: %v\n", err)
		os.Exit(1)
	}

	// 0600: backup includes account balances, session records, and other
	// per-user state — read access is restricted to the running user.
	if err := os.WriteFile(*output, data, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Backup complete:\n")
	fmt.Printf("  Block height: %d\n", b.BlockHeight)
	fmt.Printf("  Block hash:   %s\n", b.BlockHash)
	fmt.Printf("  State entries: %d\n", len(b.State))
	fmt.Printf("  Blocks: %d\n", b.Blocks)
	fmt.Printf("  Index entries: %d\n", b.Indexes)
	fmt.Printf("  Output: %s\n", *output)

}
