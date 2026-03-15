# Issues

## Resolved Issues

아래 이슈들은 코드 리뷰를 통해 발견되어 수정 완료되었다.

### P0 — 치명적

- **블록 생성 시 State 오염 + Invalid TX 영구 차단**: `ProduceBlock()`에서 block-level snapshot을 찍고 트랜잭션을 개별 실행하여 실패 tx는 건너뛴다. 실패 tx는 mempool에서 즉시 제거되고, 성공 tx만으로 최종 블록을 구성한다. `AddBlock` 실패 시 snapshot으로 rollback한다. (`consensus/poa.go`)
- **블록 P2P 브로드캐스트 누락**: `EventBlockCommit` 이벤트 data에 block 포인터를 포함시키고, `cmd/node/main.go`에서 이벤트 구독하여 `BroadcastBlock()`을 호출한다. (`consensus/poa.go`, `cmd/node/main.go`)

### P1 — 주요

- **이벤트 버퍼링 — 커밋 후 emit**: `EventEmitter` 인터페이스와 `Buffer` 구조체를 도입하여 블록 실행 중에는 이벤트를 버퍼링하고, 커밋 성공 후에만 실제 emitter로 flush한다. 롤백 시에는 discard된다. (`events/buffer.go`, `vm/executor.go`, `consensus/poa.go`, `network/sync.go`)
- **Indexer MarketBuy 미갱신**: `EventMarketBuy` 구독을 추가하여 seller에서 제거, buyer에 추가하는 `onMarketBuy` 핸들러를 등록했다. (`indexer/indexer.go`)
- **Market Listing 취소**: `TxCancelListing` 타입, `CancelListingPayload`, `handleCancelListing` 핸들러, `EventMarketCancel` 이벤트를 추가했다.
- **Session 취소**: `TxSessionCancel` 타입, `handleSessionCancel` 핸들러 추가. creator 또는 operator가 취소 가능, stakes 환불.
- **Session 플레이어 동의**: `SessionOpenPayload`에 `Signatures map[string]string` 필드를 추가. stakes > 0일 때 각 플레이어의 서명 검증.

### P2 — 보통

- **P2P ChainID 검증**: `handleTx`에서 ChainID 불일치 시 거부.
- **Sync 후 Mempool 정리**: 블록 커밋 후 해당 tx들을 mempool에서 제거.
- **Peer Idle Disconnect**: 5분 read deadline + MsgPing/MsgPong.
- **DefaultConfig Validator 자동 등록**: dev mode 키 자동 등록.
- **AddBlock Genesis 검증**: tip nil 시 height 0 강제.
- **Mempool Nonce 추적**: sender별 중복 nonce 거부.

### P3 — 개선

- **Asset Schema 검증**: 민팅 시 Properties와 Template.Schema 일치 검증. Reward 민팅에도 적용.
- **게임 서버 권한 관리**: Operators 설정, RequireOperator 전수 적용 (session_open/result/cancel, mint_asset, register_template, grant_reward, random_commit/reveal).

### 코드 리뷰 수정 (4차까지 완료)

- **토큰 supply cap**: `system:total_supply` 추적, `MaxTotalSupply` config, genesis alloc overflow 검증.
- **Executor mutex**: `sync.Mutex`로 동시성 보호, `executeTxLocked` 내부 메서드 분리.
- **MsgBlock 핸들러**: 실시간 블록 전파 (tip+1 블록만 수락, 검증→실행→커밋).
- **sync return-on-failure**: ExecuteBlock/AddBlock 실패 시 배치 중단.
- **세션 중복 플레이어 검증**: handleSessionOpen에서 중복 pubkey 거부.
- **commit-reveal 최소 간격**: 2블록 간격 강제.
- **RPC auth subtle.ConstantTimeCompare**: 타이밍 공격 방지.
- **피어 ID re-key**: MsgHello 후 선언된 node_id로 재등록.
- **P2P per-peer rate limit**: 100 msg/s 초과 시 연결 해제.
- **SSE 동시 접속 제한**: 최대 100 클라이언트.
- **CORS 조건부 설정**: auth 토큰 설정 시 wildcard 제거.
- **session_cancel 권한**: 생성자 항상 취소 가능 + 비생성자는 오퍼레이터만.
- **Mempool compaction**: cap > 2*len+64 시 재할당.
- **Indexer O(1)**: JSON 배열 → prefix-key 방식, iterator 기반 조회.
- **블록 프루닝**: `PruneKeepBlocks` config, genesis 보존, 증분 프루닝.
- **LevelDB 손상 복구**: `RecoverFile` 자동 시도.
- **로그 레벨 config**: `LogLevel` 필드 + slog 레벨 설정.
- **MaxBlockTxs 상한**: 10000 제한.
- **genesis alloc vs supply cap**: 초과 시 거부.
- **타임스탬프 양수 검증**: 블록 타임스탬프 > 0 강제.
- **세션/보상 수량 제한**: maxSessionPlayers=10, maxRewardAssets=50.
- **오퍼레이터 체크 일관화**: 모든 모듈 `len(ctx.Operators) > 0` 또는 `RequireOperator`.
- **ComputeRoot iterator 에러 체크**: 로깅 추가.
- **Genesis TxRoot**: `core.ComputeTxRoot(nil)` 사용.
- **Mempool 메트릭 정확도**: 실제 삭제 수만 카운트.
- **문서화**: PoA 신뢰 모델, ComputeRoot O(N), /metrics 인증, SSE 리버스 프록시, RPC 상태 비격리.

---

## Open Issues

현재 열린 이슈 없음. 로깅은 slog로 전환 완료, 테스트 커버리지는 패키지별로 추가 완료.

### 보류 (2.0 범위)

1. **오퍼레이터 multi-sig**: 현재 단일 키 의존. 외부 오퍼레이터나 DAO 거버넌스 필요 시 구현.
2. **증분 Merkle 구조**: ComputeRoot O(N) → Patricia Trie. 상태가 수십만 건 이상일 때 필요.
