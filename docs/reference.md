# TOL Chain Reference

Detailed reference for node operators and game server integrators. See the [README](../README.md) for an overview.

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
    "timestamp": 1735689600000000000,
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
| `node_id` | - | Unique node identifier (required) |
| `data_dir` | `./data` | LevelDB storage directory |
| `rpc_port` | `8545` | JSON-RPC HTTP port |
| `p2p_port` | `30303` | P2P TCP port |
| `max_block_txs` | `500` | Max transactions per block |
| `validators` | - | Validator public keys (required) |
| `operators` | - | Game operator keys (empty = no restriction) |
| `genesis.timestamp` | `1735689600000000000` | Genesis block timestamp (Unix ns). Part of the genesis hash - must be identical on every node |
| `rpc_auth_token` | - | Bearer token for RPC auth (empty = no auth) |
| `log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `max_total_supply` | `0` | Hard cap on total minted tokens (0 = unlimited) |
| `prune_keep_blocks` | `0` | Keep only the latest N blocks (0 = no pruning). Must be 0 or at least 102, because `random_reveal` reads the hash of a block up to 102 blocks back |

Optional sections `mempool`, `network`, `rpc`, and `consensus` tune limits such as pool size, peer count, timeouts, rate limit (`rpc.rate_limit` 100/s, `rpc.rate_burst` 200), and block interval (`consensus.block_interval_sec` 2). Defaults are in `config/config.go`.

Environment variable: `TOL_PASSWORD`, the keystore encryption password.

## RPC API

All requests: `POST /` with JSON-RPC 2.0.
Auth: `Authorization: Bearer <token>` (if configured).

### Query Methods

| Method | Params | Returns |
|--------|--------|---------|
| `getBlockHeight` | - | `int64` |
| `getBlock` | `hash` or `height` | Block |
| `getBalance` | `address` | `{balance, nonce}` |
| `getAsset` | `id` | Asset |
| `getTemplate` | `id` | AssetTemplate |
| `getSession` | `id` | Session |
| `getListing` | `id` | MarketListing |
| `getInventory` | `owner` | Inventory |
| `getRandomCommitment` | `id` | RandomCommitment |
| `getTxStatus` | `tx_id` | TxStatus |
| `getMempoolSize` | - | `int` |
| `getAssetsByOwner` | `owner, offset?, limit?` | `[]string` or paginated |
| `getSessionsByPlayer` | `player, offset?, limit?` | `[]string` or paginated |
| `getActiveListings` | `offset?, limit?` | `[]string` or paginated |
| `getDelegation` | `granter, grantee` | DelegationGrant |
| `getDelegationsByGranter` | `granter` | `[]DelegationGrant` |
| `getDelegationsByGrantee` | `grantee` | `[]DelegationGrant` |

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

## Event System

Event types streamed via SSE at `GET /events`:

`block_commit` · `tx_executed` · `token_transfer` · `asset_minted` · `asset_burned` · `asset_transfer` · `template_registered` · `session_open` · `session_close` · `session_cancel` · `market_list` · `market_buy` · `market_cancel` · `item_equipped` · `item_unequipped` · `reward_granted` · `random_commit` · `random_reveal` · `delegation_granted` · `delegation_revoked`

Events are buffered during block execution and emitted only after successful commit.

## Monitoring

Prometheus metrics are exposed at `/metrics` on the RPC port. Metric names carry the `tolchain_` namespace prefix.

| Category | Metrics |
|----------|---------|
| **Consensus** | `blocks_produced_total`, `block_height`, `block_production_duration_seconds`, `block_tx_count`, `tx_failed_total` |
| **Mempool** | `mempool_size`, `mempool_added_total`, `mempool_removed_total`, `mempool_rejected_total{reason}` |
| **Network** | `peers_connected`, `p2p_messages_received_total{type}` |
| **RPC** | `rpc_requests_total{method}`, `rpc_request_duration_seconds{method}`, `rpc_errors_total{code}`, `rpc_rate_limited_total` |

`docker compose up -d` starts Prometheus (`:9090`) and Grafana (`:3000`, admin/admin) alongside the node.

## Security

- **Keystore**: PBKDF2 (210,000 iterations) + AES-256-GCM + random nonce, file permission 0600
- **P2P**: 10 MB message limit, 5 min read timeout, 30 sec write timeout, max 50 peers
- **RPC**: Bearer token auth, 1 MB body limit, IP rate limiting (100/s burst 200)
- **State**: Mutex-protected StateDB, deep-copy snapshots, nonce-based replay prevention
- **Chain**: ChainID cross-chain replay prevention, 15 sec timestamp drift limit, monotonic block timestamps

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
1. Operator: random_commit(hash(secret)) at height H
2. Within blocks H+3 .. H+102: random_reveal(secret)
3. Result = hash(secret + hash of block H+2)

The target block does not exist at commit time, and the result does not
depend on which block carries the reveal, so the committer cannot pick an
outcome by timing the reveal. The committer can still withhold the reveal;
treat a commitment that is unrevealed after H+102 as forfeited.
```

## Changes

This result rule replaced the earlier `hash(secret + PrevHash of the reveal
block)` without an activation height. A chain that already holds
`random_reveal` transactions from the old rule must be regenerated: replaying
it under the new rule yields different results and state roots.
