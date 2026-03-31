# TOL Chain

Game-specialized private blockchain written in Go.
Assets, sessions, markets, inventory, rewards, and verifiable randomness — all on-chain.

[![CI](https://github.com/tolelom/tolchain/actions/workflows/ci.yml/badge.svg)](https://github.com/tolelom/tolchain/actions/workflows/ci.yml)
![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)

---

## Overview

TOL Chain is a Proof-of-Authority blockchain designed for MMORPG backends.
Game servers submit signed transactions for item drops, trades, and session results;
the chain provides a tamper-proof, auditable record without requiring a full public consensus protocol.

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25 |
| Consensus | PoA round-robin |
| Signature | ed25519 (stdlib) |
| Hash | SHA-256 (stdlib) |
| Storage | LevelDB |
| Key encryption | PBKDF2 + AES-256-GCM |
| RPC | JSON-RPC 2.0 + SSE |
| P2P | TCP + length-prefixed JSON |
| Monitoring | Prometheus + Grafana |
| External deps | 2 (goleveldb, x/crypto) |
| CI/CD | GitHub Actions → GHCR → SSH deploy |

---

## Features

- **18 transaction types** across 8 pluggable VM modules
- **Asset management** — mint, burn, transfer with schema-validated templates
- **In-game marketplace** — list / buy / cancel with equipped-item blocking
- **Game sessions** — stake locking, multi-player consent signatures, reward distribution
- **Inventory** — slot-based equipment with trade/burn protection while equipped
- **Batch rewards** — atomic token + asset grants (operator-only)
- **Commit-reveal randomness** — `hash(secret + prevBlockHash)` for fair drops
- **SSE event streaming** — real-time events with type filtering (21 event types)
- **IP-based rate limiting** — token bucket (100 req/s, burst 200) per IP
- **Prometheus metrics** — 15 custom metrics across consensus, mempool, network, RPC
- **Snapshot/rollback** — per-tx and per-block state rollback on failure
- **mTLS support** — optional mutual TLS for P2P connections

---

## Quick Start

### Dev Mode (no config file needed)

```bash
# Generate a validator key
export TOL_PASSWORD=my-secret
go run ./cmd/node --genkey

# Start the node (auto-creates genesis with the loaded key)
go run ./cmd/node
```

The node starts on `:8545` (RPC) and `:30303` (P2P).

```bash
# Test RPC
curl -s -X POST http://localhost:8545 \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"getBlockHeight","params":{},"id":1}'

# Check node status
curl -s http://localhost:8545/status | jq
```

### Docker

```bash
# Automated single-node setup
./scripts/setup.sh

# Start all services (node + Prometheus + Grafana)
docker compose up -d

# Logs
docker compose logs -f tolchain

# Stop
docker compose down
```

### Build from Source

```bash
make build              # current OS/arch
make darwin-arm64       # macOS Apple Silicon
make linux-amd64        # Linux x86_64
make test               # run tests
make vet                # static analysis
```

---

## Configuration

Create `config.json` (or omit for dev defaults):

```json
{
  "node_id": "node0",
  "data_dir": "./data",
  "rpc_port": 8545,
  "p2p_port": 30303,
  "max_block_txs": 500,
  "rpc_auth_token": "optional-bearer-token",
  "validators": ["<ed25519-pubkey-hex>"],
  "operators": ["<game-server-pubkey-hex>"],
  "genesis": {
    "chain_id": "tolchain-mainnet",
    "alloc": {
      "<pubkey-hex>": 1000000
    }
  },
  "seed_peers": [
    { "id": "node1", "addr": "10.0.0.2:30303" }
  ],
  "tls": {
    "ca_cert": "certs/ca.crt",
    "node_cert": "certs/node.crt",
    "node_key": "certs/node.key"
  }
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `node_id` | — | Unique node identifier (required) |
| `data_dir` | `./data` | LevelDB storage directory |
| `rpc_port` | `8545` | JSON-RPC HTTP port |
| `p2p_port` | `30303` | P2P TCP port |
| `max_block_txs` | `500` | Max transactions per block |
| `validators` | — | Validator public keys (required) |
| `operators` | — | Game operator keys (empty = no restriction) |
| `rpc_auth_token` | — | Bearer token for RPC auth (empty = no auth) |

Environment variable: `TOL_PASSWORD` — keystore encryption password.

---

## RPC API

All requests: `POST /` with JSON-RPC 2.0.
Auth: `Authorization: Bearer <token>` (if configured).

### Query Methods

| Method | Params | Returns |
|--------|--------|---------|
| `getBlockHeight` | — | `int64` |
| `getBlock` | `hash` or `height` | Block |
| `getBalance` | `address` | `{balance, nonce}` |
| `getAsset` | `id` | Asset |
| `getTemplate` | `id` | AssetTemplate |
| `getSession` | `id` | Session |
| `getListing` | `id` | MarketListing |
| `getInventory` | `owner` | Inventory |
| `getRandomCommitment` | `id` | RandomCommitment |
| `getTxStatus` | `tx_id` | TxStatus |
| `getMempoolSize` | — | `int` |
| `getAssetsByOwner` | `owner, offset?, limit?` | `[]string` or paginated |
| `getSessionsByPlayer` | `player, offset?, limit?` | `[]string` or paginated |
| `getActiveListings` | `offset?, limit?` | `[]string` or paginated |

### Write Method

| Method | Params | Description |
|--------|--------|-------------|
| `sendTx` | signed transaction | Submit to mempool |

### Additional Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/status` | GET | Node status JSON (no auth) |
| `/events` | GET | SSE event stream (`?types=asset_minted,market_buy`) |
| `/metrics` | GET | Prometheus metrics |

---

## Transaction Types

| Type | Module | Permission | Description |
|------|--------|-----------|-------------|
| `transfer` | economy | anyone | Token transfer |
| `register_template` | asset | operator | Define asset schema |
| `mint_asset` | asset | operator | Create asset from template |
| `burn_asset` | asset | owner | Destroy asset (blocked if equipped) |
| `transfer_asset` | asset | owner | Transfer asset (blocked if equipped) |
| `session_open` | session | operator | Start game session with stakes |
| `session_result` | session | operator | Close session, distribute rewards |
| `session_cancel` | session | operator | Cancel session, refund stakes |
| `list_market` | market | owner | List asset for sale (blocked if equipped) |
| `buy_market` | market | anyone | Purchase listed asset |
| `cancel_listing` | market | owner | Remove market listing |
| `equip_item` | inventory | owner | Assign asset to slot |
| `unequip_item` | inventory | owner | Remove asset from slot |
| `grant_reward` | reward | operator | Batch token + asset grant |
| `random_commit` | random | operator | Submit hash commitment |
| `random_reveal` | random | operator | Reveal secret, compute result |
| `grant_delegation` | delegation | owner | Grant operator permission to another key |
| `revoke_delegation` | delegation | owner | Revoke granted delegation |

---

## Event System

21 event types streamed via SSE at `GET /events`:

`block_commit` · `tx_executed` · `token_transfer` · `asset_minted` · `asset_burned` · `asset_transfer` · `template_registered` · `session_open` · `session_close` · `session_cancel` · `market_list` · `market_buy` · `market_cancel` · `item_equipped` · `item_unequipped` · `reward_granted` · `random_commit` · `random_reveal` · `delegation_granted` · `delegation_revoked`

Events are buffered during block execution and emitted only after successful commit.

---

## Monitoring

Prometheus metrics are exposed at `/metrics` on the RPC port.

| Category | Metrics |
|----------|---------|
| **Consensus** | `blocks_produced_total`, `block_height`, `block_production_duration_seconds`, `block_tx_count`, `tx_failed_total` |
| **Mempool** | `mempool_size`, `mempool_added_total`, `mempool_removed_total`, `mempool_rejected_total{reason}` |
| **Network** | `peers_connected`, `p2p_messages_received_total{type}` |
| **RPC** | `rpc_requests_total{method}`, `rpc_request_duration_seconds{method}`, `rpc_errors_total{code}`, `rpc_rate_limited_total` |

`docker compose up -d` starts Prometheus (`:9090`) and Grafana (`:3000`, admin/admin) alongside the node.

---

## Project Structure

```
tolchain/
├── cmd/node/           # Entry point, CLI flags, graceful shutdown
├── config/             # Config loading, validation, genesis block
├── consensus/          # PoA round-robin block production & validation
├── core/               # Block, Transaction, Account, Asset, Mempool
├── crypto/             # SHA-256, ed25519, TLS cert generation
├── events/             # Pub/Sub emitter with commit-deferred buffering
├── indexer/            # Secondary indexes (owner→assets, player→sessions)
├── internal/testutil/  # In-memory DB & block store for tests
├── metrics/            # Prometheus metric definitions
├── monitoring/         # Prometheus/Grafana configuration
├── network/            # TCP P2P, peer management, block sync
├── rpc/                # JSON-RPC 2.0 server, rate limiter, SSE broker
├── scripts/            # Deployment automation
├── storage/            # LevelDB wrapper, StateDB with snapshot/rollback
├── tests/              # Integration tests & benchmarks
├── vm/                 # Transaction executor & handler registry
│   └── modules/        # asset, delegation, economy, inventory, market, random, reward, session
└── wallet/             # Key generation, AES-GCM keystore, tx builders
```

### Adding a New Transaction Type

1. `core/transaction.go` — add `TxType` constant + payload struct
2. `core/state.go` — add domain type + `State` interface methods (if needed)
3. `storage/statedb.go` — register prefix + implement Get/Set
4. `events/emitter.go` — add `EventType` constant
5. `vm/modules/<name>/` — implement handler + `init()` registration
6. `rpc/handler.go` — add query RPC method
7. `wallet/wallet.go` — add tx builder helper
8. `cmd/node/main.go` — add blank import
9. `tests/` — write tests (with blank imports for VM modules)

---

## Testing

```bash
# All tests
go test ./tests/ -v

# With coverage
go test ./tests/ -coverprofile=coverage.out -coverpkg=./...
go tool cover -func=coverage.out | tail -1

# Benchmarks
go test ./tests/ -bench=. -benchmem -run=^$
```

CI enforces a **50% minimum coverage** gate.

---

## Security

- **Keystore**: PBKDF2 (210,000 iterations) + AES-256-GCM + random nonce, file permission 0600
- **P2P**: 10 MB message limit, 5 min read timeout, 30 sec write timeout, max 50 peers
- **RPC**: Bearer token auth, 1 MB body limit, IP rate limiting (100/s burst 200)
- **State**: Mutex-protected StateDB, deep-copy snapshots, nonce-based replay prevention
- **Chain**: ChainID cross-chain replay prevention, 15 sec timestamp drift limit, monotonic block timestamps

---

## Game Integration Examples

### Item Drop (Boss Kill)

```
Game Server → Web Server → TOL Chain: grant_reward tx (tokens + asset)
TOL Chain → SSE: reward_granted event → Web Server → Unity UI update
```

### Marketplace Trade

```
Player A → list_market tx (blocked if item is equipped)
Player B → buy_market tx → automatic asset transfer + payment
SSE: market_buy event → both players notified in real-time
```

### Fair Random Drop

```
1. Operator: random_commit(hash(secret))
2. At drop time: random_reveal(secret)
3. Result = hash(secret + block.PrevHash) → tamper-proof
```

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## License

This project is licensed under the MIT License — see [LICENSE](LICENSE) for details.
