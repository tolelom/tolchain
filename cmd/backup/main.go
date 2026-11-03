// Command backup exports world state from a stopped node's LevelDB (or a copy
// of it) to JSON. The database is opened read-only.
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
	"github.com/tolelom/tolchain/storage"
)

type stateEntry struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type backup struct {
	BlockHeight int64        `json:"block_height"`
	BlockHash   string       `json:"block_hash"`
	ChainTip    string       `json:"chain_tip"`
	State       []stateEntry `json:"state"`
	Blocks      int          `json:"blocks_count"`
	Indexes     int          `json:"indexes_count"`
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
	defer func() { _ = db.Close() }()

	b := backup{}

	if tipVal, err := db.Get([]byte(storage.ChainTipKey), nil); err == nil {
		b.ChainTip = string(tipVal)
	}

	if b.ChainTip != "" {
		if blockData, err := db.Get([]byte(storage.BlockKeyPrefix+b.ChainTip), nil); err == nil {
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

	for _, prefix := range storage.StatePrefixes() {
		iter := db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
		for iter.Next() {
			key := string(iter.Key())
			val := make([]byte, len(iter.Value()))
			copy(val, iter.Value())

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

	countPrefix := func(prefix string) int {
		count := 0
		iter := db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
		for iter.Next() {
			count++
		}
		iter.Release()
		return count
	}

	b.Blocks = countPrefix(storage.BlockKeyPrefix)
	b.Indexes = countPrefix("idx:")

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal backup: %v\n", err)
		os.Exit(1)
	}

	// 0600: the export holds every account's state.
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
