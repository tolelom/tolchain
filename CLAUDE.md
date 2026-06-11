# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make build                  # 현재 OS/arch용 빌드 → tolchain-node
make test                   # 전체 테스트 (go test ./...)
make vet                    # 정적 분석 (go vet)
make darwin-arm64           # Mac M시리즈 크로스 컴파일
make linux-amd64            # Linux x86_64 크로스 컴파일
make clean                  # 빌드 아티팩트 제거

# Docker
docker-compose up --build   # Node + Prometheus + Grafana 스택
```

## Tech Stack

- **Go 1.24** — 엔트리 포인트: `cmd/node/`
- **LevelDB** (goleveldb) — 키-값 저장소
- **ed25519** — 트랜잭션/블록 서명
- **Prometheus** — 메트릭 수집
- **mTLS** — 선택적 P2P 보안 통신

## Project Purpose

**TOL Chain** — 게임 특화 Proof-of-Authority(PoA) 블록체인.
MMORPG "One of the plans"의 인게임 자산, 토큰 전송, NFT 마켓, 세션 기록을 위변조 불가능하게 기록.

## Project Structure

```
cmd/node/           # 진입점, CLI 플래그, graceful shutdown
config/             # 설정 로딩, 검증, 제네시스 블록
consensus/          # PoA 라운드 로빈 합의
core/               # Block, Transaction, Account, Asset, Mempool, Blockchain
crypto/             # SHA-256 해싱, ed25519 서명, TLS 인증서 생성
events/             # Pub/Sub 이벤트 시스템, commit-deferred 버퍼링
indexer/            # 보조 인덱스 (owner→assets, player→sessions, active listings)
internal/testutil/  # 테스트용 인메모리 DB & 블록스토어
metrics/            # Prometheus 메트릭 정의
monitoring/         # Prometheus/Grafana 설정 파일
network/            # TCP P2P, 피어 관리, 블록 싱크
rpc/                # JSON-RPC 2.0 서버, Rate Limiter, SSE
scripts/            # 배포 자동화 (setup.sh)
storage/            # LevelDB 래퍼, StateDB (스냅샷/롤백)
tests/              # 통합 테스트 & 벤치마크
vm/                 # 트랜잭션 실행기 & 핸들러 레지스트리
└── modules/        # 7개 플러거블 모듈
wallet/             # 키 생성, AES-GCM 키스토어, 트랜잭션 빌더
```

## 핵심 아키텍처

### 합의 (PoA 라운드 로빈)

- 검증자가 `height % len(validators)` 순서로 블록 생성
- `ProduceBlock()`: 블록 빌드 → 트랜잭션 실행 (실패 시 개별 롤백) → 서명
- 블록 레벨 스냅샷: `AddBlock` 실패 시 전체 상태 롤백
- 이벤트 버퍼링: 실행 중 이벤트 수집, 커밋 후에만 발행, 롤백 시 폐기

### VM — 플러거블 모듈 아키텍처

7개 모듈이 `init()` 블랭크 임포트로 자동 등록:

| 모듈 | 트랜잭션 타입 | 역할 |
|------|-------------|------|
| **economy** | transfer | 토큰 전송 (오버플로우 보호) |
| **asset** | mint_asset, burn_asset, transfer_asset, register_template | NFT 자산 관리 + 스키마 검증 |
| **session** | session_open, session_result, session_cancel | 게임 세션 (스테이크 잠금, 다중 플레이어 동의 서명) |
| **market** | list_market, buy_market, cancel_listing | NFT 마켓플레이스 (장착 아이템 등록 차단) |
| **inventory** | equip_item, unequip_item | 슬롯 기반 장비 관리 |
| **random** | random_commit, random_reveal | 커밋-리빌 난수 생성 (`hash(secret + prevBlockHash)`) |
| **reward** | grant_reward | 원자적 토큰 + 자산 일괄 지급 (오퍼레이터 전용) |

**Executor 흐름**: 서명 검증 → 수수료 징수 → 핸들러 디스패치 → 실패 시 스냅샷 롤백

### 상태 관리 (StateDB)

- **LevelDB 백엔드** + 인메모리 쓰기 버퍼
- **접두사 네임스페이스**: `acct:`, `asset:`, `tmpl:`, `sess:`, `list:`, `inv:`, `rcom:`
- **스냅샷/롤백**: 딥카피 스냅샷 스택, 실패 시 롤백
- **StateRoot**: 커밋 전 `ComputeRoot()` — 결정적 해시 계산

### Mempool

- 스레드 세이프, 최대 10,000 트랜잭션
- 논스 추적으로 중복 방지
- 타임스탬프 검증: ±1시간 / -5분

### 네트워크 (P2P)

- **TCP** 길이 접두사 JSON 메시지
- 메시지 타입: `MsgTx`, `MsgBlock`, `MsgPing`, `MsgBlocks`
- 최대 50 피어, 메시지 크기 제한 10MB
- 선택적 mTLS (CA cert, node cert, node key)
- 블록 싱크: 피어와 온디맨드 동기화

### RPC API

**JSON-RPC 2.0** — `/` 엔드포인트:

조회:
- `getBlockHeight`, `getBlock`, `getBalance`, `getAsset`, `getTemplate`
- `getSession`, `getListing`, `getInventory`, `getRandomCommitment`
- `getTxStatus`, `getMempoolSize`
- `getAssetsByOwner`, `getSessionsByPlayer`, `getActiveListings` (페이지네이션)

쓰기:
- `sendTx`

추가 엔드포인트:
- `GET /status` — 노드 상태 (인증 불필요)
- `GET /events` — SSE 이벤트 스트림 (타입 필터링)
- `GET /metrics` — Prometheus 메트릭

Rate Limit: 토큰 버킷 100 req/s, 버스트 200 (IP당)

### 이벤트 시스템

19개 이벤트 타입의 Pub/Sub 브로커:
- 블록: `block_commit`
- 트랜잭션: `tx_executed`
- 토큰: `token_transfer`
- 자산: `asset_minted`, `asset_burned`, `asset_transferred`, `template_registered`
- 세션: `session_open`, `session_close`, `session_cancel`
- 마켓: `market_list`, `market_buy`, `market_cancel`
- 인벤토리: `item_equipped`, `item_unequipped`
- 보상: `reward_granted`
- 난수: `random_commit`, `random_reveal`

이벤트 구독으로 인덱서가 보조 인덱스 자동 갱신.

### 인덱서

이벤트 기반 보조 인덱스:
- owner → assets, player → sessions, active listings, tx results
- 페이지네이션: 기본 50, 최대 200

## 암호화

- **서명**: ed25519 (트랜잭션, 블록)
- **해싱**: SHA-256 (블록 해시, 트랜잭션 해시)
- **키스토어**: PBKDF2 (210k iterations) + AES-256-GCM, 파일 권한 0600
- **주소**: SHA-256(pubkey)의 앞 20바이트 → 40자 hex

## 보안

- **리플레이 방지**: ChainID, 논스 추적, 15초 타임스탬프 드리프트
- **접근 제어**: 오퍼레이터 전용 트랜잭션 (mint, reward, template)
- **상태 안전성**: Mutex 보호 StateDB, 딥카피 스냅샷
- **P2P**: mTLS, 메시지 크기/피어 수 제한

## 설정 (config.json)

```json
{
  "node_id": "...",
  "data_dir": "./data",
  "rpc_port": 8545,
  "p2p_port": 6060,
  "max_block_txs": 1000,
  "validators": ["<pubkey_hex>"],
  "operators": ["<address_hex>"],
  "genesis": { "chain_id": "tolchain-dev", "alloc": {}, "timestamp": 1735689600000000000 },
  "seed_peers": ["host:port"],
  "tls": { "ca_cert": "", "node_cert": "", "node_key": "" },
  "rpc_auth_token": "..."
}
```

Dev 모드: 설정 파일 없으면 단일 노드 설정 자동 생성.

## 테스트

```bash
make test                           # 전체 테스트
go test ./tests/...                 # 통합 테스트만
go test -bench=. ./tests/...        # 벤치마크
```

- `internal/testutil/` — 인메모리 DB & 블록스토어 (테스트용)
- CI: GitHub Actions, 최소 커버리지 50% 게이트

## Prometheus 메트릭

- **합의**: `blocks_produced_total`, `block_height`, `block_production_duration_seconds`, `block_tx_count`, `tx_failed_total`
- **Mempool**: `mempool_size`, `mempool_added/removed/rejected_total`
- **네트워크**: `peers_connected`, `p2p_messages_received_total{type}`
- **RPC**: `rpc_requests_total{method}`, `rpc_request_duration_seconds`, `rpc_errors_total{code}`, `rpc_rate_limited_total`
