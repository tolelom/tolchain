# Issues

## Resolved Issues

아래 이슈들은 코드 리뷰를 통해 발견되어 수정 완료되었다.

### P0 — 치명적

- **블록 생성 시 State 오염 + Invalid TX 영구 차단**: `ProduceBlock()`에서 block-level snapshot을 찍고 트랜잭션을 개별 실행하여 실패 tx는 건너뛴다. 실패 tx는 mempool에서 즉시 제거되고, 성공 tx만으로 최종 블록을 구성한다. `AddBlock` 실패 시 snapshot으로 rollback한다. (`consensus/poa.go`)
- **블록 P2P 브로드캐스트 누락**: `EventBlockCommit` 이벤트 data에 block 포인터를 포함시키고, `cmd/node/main.go`에서 이벤트 구독하여 `BroadcastBlock()`을 호출한다. (`consensus/poa.go`, `cmd/node/main.go`)

### P1 — 주요

- **이벤트 버퍼링 — 커밋 후 emit**: `EventEmitter` 인터페이스와 `Buffer` 구조체를 도입하여 블록 실행 중에는 이벤트를 버퍼링하고, 커밋 성공 후에만 실제 emitter로 flush한다. 롤백 시에는 discard된다. (`events/buffer.go`, `vm/executor.go`, `consensus/poa.go`, `network/sync.go`)
- **Indexer MarketBuy 미갱신**: `EventMarketBuy` 구독을 추가하여 seller에서 제거, buyer에 추가하는 `onMarketBuy` 핸들러를 등록했다. (`indexer/indexer.go`)
- **Market Listing 취소**: `TxCancelListing` 타입, `CancelListingPayload`, `handleCancelListing` 핸들러, `EventMarketCancel` 이벤트를 추가했다. seller 검증, listing 비활성화, asset의 `ActiveListingID` 초기화를 수행한다. (`core/transaction.go`, `vm/modules/market/market.go`, `events/emitter.go`)
- **Session 취소**: `TxSessionCancel` 타입, `SessionCancelPayload`, `handleSessionCancel` 핸들러, `EventSessionCancel` 이벤트를 추가했다. creator 검증 후 모든 플레이어에게 stakes를 환불하고 status를 "cancelled"로 변경한다. (`core/transaction.go`, `vm/modules/session/session.go`, `events/emitter.go`)
- **Session 플레이어 동의**: `SessionOpenPayload`에 `Signatures map[string]string` 필드를 추가했다. stakes > 0일 때 tx.From을 제외한 각 플레이어가 `"session:<sessionID>"` 메시지에 서명했는지 검증한다. (`core/transaction.go`, `vm/modules/session/session.go`)

### P2 — 보통

- **P2P ChainID 검증**: `Node`에 `chainID` 필드를 추가하고 `handleTx`에서 `tx.ChainID != n.chainID`이면 거부한다. (`network/node.go`, `cmd/node/main.go`)
- **Sync 후 Mempool 정리**: `Syncer`에 `mempool` 필드를 추가하고, `handleBlocks`에서 블록 커밋 후 해당 tx들을 mempool에서 제거한다. (`network/sync.go`)
- **Peer Idle Disconnect (Ping/Pong)**: Read deadline을 30초에서 5분으로 증가시키고, `MsgPing`/`MsgPong` 메시지 타입과 ping 핸들러를 추가했다. (`network/peer.go`, `network/node.go`)
- **DefaultConfig Validator 자동 등록**: config 파일이 없을 때 validators가 비어있으면 로드된 키를 자동 등록한다 (dev mode). (`cmd/node/main.go`)
- **AddBlock Genesis 검증**: `tip == nil`일 때 `block.Header.Height != 0`이면 거부한다. (`core/blockchain.go`)
- **Mempool Nonce 추적**: sender별 pending nonce set을 추가하여 같은 sender의 중복 nonce를 거부한다. `Remove()` 시 nonce도 정리된다. (`core/mempool.go`)

### P3 — 개선

- **Asset Schema 검증**: 민팅 시 Properties가 Template.Schema와 일치하는지 검증한다. 지원 타입은 "string", "int", "bool"이며, 스키마에 없는 extra property는 거부된다. (`vm/modules/asset/asset.go`)
- **게임 서버 권한 관리**: `Config`에 `Operators []string` 필드를 추가하고, `Executor`에 operators map을 전달하여 `Context`에 포함시킨다. template 등록, session open/cancel에서 operator 검증을 수행한다 (비어있으면 제한 없음). (`config/config.go`, `vm/executor.go`, `vm/modules/asset/template.go`, `vm/modules/session/session.go`, `cmd/node/main.go`)

---

## Open Issues

### 1. Migrate logging to `log/slog` (structured logging)

**Priority:** Medium
**Scope:** All packages

현재 모든 로깅이 `log.Printf`를 사용 중. 프로덕션에서는 레벨 구분(debug/info/warn/error), JSON 출력, 로그 수집(ELK/Loki) 연동이 필요.

#### 작업 내용
- `log.Printf` → `slog.Info`/`slog.Warn`/`slog.Error` 전환
- `log.Fatalf` → `slog.Error` + `os.Exit(1)` 또는 유지
- `cmd/node/main.go`에서 로그 레벨/포맷 설정 (JSON vs text)
- config에 `log_level` 필드 추가

#### 영향 파일
consensus/poa.go, network/node.go, network/sync.go, indexer/indexer.go, rpc/server.go, cmd/node/main.go 등 전체

---

### 2. Add test coverage for core packages

**Priority:** Medium
**Scope:** consensus, storage, config, wallet

현재 테스트가 `tests/` 패키지에만 존재. 핵심 패키지별 유닛 테스트가 전무.

#### 필요한 테스트
- **consensus**: `IsProposer()` 라운드로빈, `ProduceBlock()` 정상/실패/실패TX건너뛰기, `ValidateBlock()` 정상/서명불일치/높이불일치
- **storage**: `StateDB.Snapshot()`/`RevertToSnapshot()`/`Commit()` 라운드트립, `ComputeRoot()` 결정론적 해시, `LevelBlockStore.CommitBlock()` 원자성
- **config**: `Load()` + `Validate()` — 유효/무효 케이스, `DefaultConfig()` 동작, `Save()` 권한 확인
- **wallet**: `SaveKey`/`LoadKey` 라운드트립, 잘못된 비밀번호 거부, 빈 비밀번호 동작
- **mempool**: nonce 중복 거부, Remove 시 nonce 정리
- **events**: Buffer flush/discard 동작
- **market**: cancel_listing 정상/권한오류
- **session**: session_cancel 환불, 플레이어 동의 서명 검증

#### 참고
- `internal/testutil`의 `MemDB`/`MemBlockStore` 활용
- 각 패키지 내부에 `_test.go` 생성 (package-level 테스트)
