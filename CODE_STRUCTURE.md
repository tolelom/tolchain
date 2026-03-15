# TOL Chain — 코드 구조 분석

## 프로젝트 개요

**TOL Chain**은 MMORPG/로그라이크 게임을 위한 Go 블록체인입니다. PoA(Proof-of-Authority) 합의, 온체인 자산 관리, 게임 세션 스테이킹, P2P 자산 거래를 지원합니다.

- **모듈 경로:** `github.com/tolelom/tolchain`
- **Go 버전:** 1.22
- **Go 파일:** 38개, 약 4,369줄
- **외부 의존성:** goleveldb, x/crypto

---

## 디렉토리 구조

```
tolchain/
├── cmd/node/              # 노드 부트스트랩 및 진입점 (main.go)
├── config/                # 설정, 제네시스, TLS 로딩
├── consensus/             # PoA 합의 엔진 (라운드로빈)
├── core/                  # 핵심 타입 (Transaction, Block, State, Mempool)
├── crypto/                # ed25519 서명, SHA-256 해시
│   └── certgen/           # TLS 인증서 생성
├── events/                # 이벤트 Pub/Sub 시스템
├── indexer/               # 보조 인덱스 (owner→assets, player→sessions)
├── internal/testutil/     # 테스트 전용 인메모리 DB
├── network/               # TCP P2P 네트워킹 (length-prefix + JSON)
├── rpc/                   # JSON-RPC 2.0 HTTP 서버
├── storage/               # LevelDB + StateDB (스냅샷/롤백)
├── tests/                 # 통합 테스트
├── vm/                    # 트랜잭션 실행 엔진
│   └── modules/           # 플러그형 핸들러
│       ├── asset/         # 자산 발행/소각/전송
│       ├── economy/       # 토큰 전송
│       ├── market/        # 마켓플레이스
│       └── session/       # 게임 세션
└── wallet/                # 키 생성, 암호화 키스토어, Tx 빌더
```

---

## 패키지별 상세

### 1. `core/` (4파일, 374줄) — 핵심 데이터 구조

| 파일 | 핵심 타입/함수 | 설명 |
|------|---------------|------|
| `transaction.go` | `Transaction`, `TxType`, `Hash()`, `Sign()`, `Verify()` | 트랜잭션 구조체. From = 64자 hex pubkey. 9가지 TxType (transfer, mint_asset 등) |
| `block.go` | `Block`, `BlockHeader`, `ComputeHash()`, `ComputeTxRoot()` | 블록 구조체. Length-prefix TxRoot로 경계 모호성 방지 |
| `blockchain.go` | `BlockStore` 인터페이스, `Blockchain`, `AddBlock()` | 블록 체인 관리. RWMutex 동시성. 원자적 커밋 |
| `mempool.go` | `Mempool`, `Add()`, `Pending()` | 대기 트랜잭션 풀. 최대 10,000개. 타임스탬프 ±1시간 검증 |
| `state.go` | `State` 인터페이스, `Account`, `Asset`, `Session`, `MarketListing` | 상태 인터페이스 정의. 스냅샷/롤백/커밋 지원 |

**주요 타입:**
- `Account`: Address, Balance(uint64), Nonce
- `Asset`: ID, TemplateID, Owner, Properties(map), Tradeable, ActiveListingID
- `AssetTemplate`: ID, Name, Schema(속성 제약), Creator
- `Session`: Players, Stakes, Status("open"/"closed"), Outcome(pubkey→reward)
- `MarketListing`: AssetID, Seller, Price, Active

### 2. `crypto/` (3파일, 113줄) — 암호화

| 파일 | 설명 |
|------|------|
| `keys.go` | ed25519 키 생성. `Address()` = SHA-256 앞 20바이트 hex (40자) |
| `signature.go` | `Sign()`, `Verify()` — ed25519 서명/검증 |
| `hash.go` | `Hash()` — SHA-256 hex 문자열 |
| `certgen/certgen.go` | mTLS용 자체서명 CA/노드 인증서 생성 (P256, TLS 1.3) |

### 3. `storage/` (3파일, 518줄) — 저장소

| 파일 | 설명 |
|------|------|
| `db.go` | `DB`, `Iterator`, `Batch` 인터페이스 (범용 KV 스토어) |
| `leveldb.go` | LevelDB 구현 + `LevelBlockStore`. 키: "block:", "height:", "chain:tip" |
| `statedb.go` | **가장 큰 파일(318줄)**. Write buffer + 스냅샷 스택. 접두사: "acct:", "asset:", "tmpl:", "sess:", "list:". `ComputeRoot()`: 전체 키 정렬 → length-prefix → SHA-256 |

**StateDB 패턴:**
```
읽기: dirty buffer → 없으면 DB에서 로드
쓰기: dirty buffer에 기록
스냅샷: dirty/deleted 깊은 복사
롤백: 스냅샷 복원
커밋: dirty/deleted → DB 원자적 배치 쓰기
```

### 4. `consensus/` (1파일, 182줄) — PoA 합의

- **라운드로빈:** `validators[height % len(validators)]`로 제안자 결정
- **블록 생산 순서:** 스냅샷 → 실행 → 상태루트 계산 → 서명 → 블록체인 추가 → 상태 커밋
- **검증:** 제안자 확인, 서명 검증, TxRoot 확인, height/hash 연속성
- **실행 루프:** 2초 간격으로 IsProposer 체크

### 5. `vm/` + `vm/modules/` (6파일, 600+줄) — 실행 엔진

**vm/registry.go** — 글로벌 핸들러 레지스트리 (싱글톤, init() 자동 등록)

**vm/executor.go** — 트랜잭션 실행기
- 수수료 선차감 → 논스 검증 → 핸들러 디스패치
- 실패 시 스냅샷 롤백

**모듈별 핸들러:**

| 모듈 | 핸들러 | 기능 |
|------|--------|------|
| `asset/asset.go` | handleMintAsset | 템플릿 기반 자산 발행. ID = SHA-256(txID+templateID) |
| | handleBurnAsset | 소유자만 소각. 활성 리스팅 있으면 거부 |
| | handleTransferAsset | 거래가능+활성리스팅 없는 자산만 전송 |
| `asset/template.go` | handleRegisterTemplate | 자산 템플릿 등록 (불변) |
| `economy/token.go` | handleTransfer | 토큰 전송. 오버플로우 체크 |
| `market/market.go` | handleListMarket | 마켓 등록. ActiveListingID 설정 |
| | handleBuyMarket | 구매: 자산→구매자, 토큰→판매자 |
| `session/session.go` | handleSessionOpen | 플레이어 스테이크 잠금, 세션 생성 |
| | handleSessionResult | 결과 제출(생성자만), 보상 분배 |

### 6. `events/` (1파일, 71줄) — 이벤트 시스템

- 이벤트 타입: BlockCommit, TxExecuted, TokenTransfer, Asset*, Template*, Session*, Market*
- `Emit()`: 동기 호출, 핸들러별 panic recovery
- core/vm에 의존하지 않음

### 7. `indexer/` (1파일, 143줄) — 보조 인덱스

- 이벤트 구독으로 자동 갱신
- `GetAssetsByOwner(owner)`: 소유자별 자산 ID 목록
- `GetSessionsByPlayer(player)`: 플레이어별 세션 목록
- 키: "idx:owner:asset:<owner>" → JSON 배열

### 8. `network/` (3파일, 371줄) — P2P 네트워킹

| 파일 | 설명 |
|------|------|
| `peer.go` | TCP 연결. 4바이트 길이 접두사 + JSON. 30초 읽기 데드라인, 32MB 제한 |
| `node.go` | 피어 관리. 최대 50개. Broadcast, 메시지 핸들러 등록 |
| `sync.go` | 블록 동기화. 최대 200블록 배치. 실패 시 상태 롤백 |

### 9. `rpc/` (3파일, 330줄) — JSON-RPC 서버

**API 메서드:**

| 메서드 | 파라미터 | 반환 |
|--------|---------|------|
| `getBlockHeight` | — | 현재 높이 (int64) |
| `getBlock` | hash 또는 height | Block |
| `getBalance` | address | {address, balance, nonce} |
| `getAsset` | id | Asset |
| `getSession` | id | Session |
| `getListing` | id | MarketListing |
| `getAssetsByOwner` | owner | []string |
| `sendTx` | Transaction | {tx_id} |
| `getMempoolSize` | — | 개수 (int64) |

- Bearer 토큰 인증 (선택)
- 1MB 요청 크기 제한
- `Dispatch()`로 HTTP 없이 직접 테스트 가능

### 10. `wallet/` (2파일, 163줄) — 지갑

- `wallet.go`: 키 쌍 래퍼. `NewTx()`, `Transfer()` 헬퍼
- `keystore.go`: AES-GCM 암호화 + PBKDF2 (100,000회). JSON 포맷 (PubKey, Salt, Nonce, CipherText)

### 11. `config/` (3파일, 207줄) — 설정

- `config.go`: JSON 설정 로드/검증. NodeID, 포트, 밸리데이터, TLS, 시드피어
- `genesis.go`: 제네시스 블록 생성 (초기 잔액 할당, 상태루트 계산)
- `tls.go`: mTLS 설정 로드 (TLS 1.3, 클라이언트 인증서 필수)

### 12. `cmd/node/` (1파일, 213줄) — 진입점

**시작 순서:**
1. 플래그 파싱 (-config, -key, -genkey, -gencerts)
2. 설정 로드 → 밸리데이터 키 로드
3. LevelDB + StateDB 초기화
4. 블록체인 초기화 (기존 팁 로드 또는 신규)
5. 제네시스 블록 생성 (신규인 경우)
6. Emitter, Indexer, Mempool, Executor 생성
7. PoA 합의 초기화
8. P2P 노드 시작 + 시드피어 연결 + 동기화
9. RPC 서버 시작
10. 합의 루프 실행 (2초 간격)
11. 시그널 대기 → 그레이스풀 셧다운

---

## 의존성 그래프

```
crypto
  └──→ core
         ├──→ storage
         │      └──→ config
         ├──→ events
         │      └──→ indexer
         └──→ vm
                └──→ vm/modules (asset, economy, market, session)

consensus ← core, storage, vm, events
network   ← core, consensus, storage, vm
rpc       ← core, storage, indexer
wallet    ← crypto, core
cmd/node  ← 모든 패키지 조합
```

**순환 의존성 없음**

---

## 핵심 설계 패턴

### 1. 인터페이스 기반 아키텍처
- `core.State`, `core.BlockStore`, `storage.DB` — 구현체 교체 가능
- 테스트: MemDB/MemBlockStore, 프로덕션: LevelDB

### 2. Write Buffer + 스냅샷/롤백
- StateDB가 dirty map + deleted set 유지
- 중첩 스냅샷 스택으로 트랜잭션 단위 롤백
- ComputeRoot()는 디스크 + dirty 버퍼 병합

### 3. 플러그형 핸들러 레지스트리
- `init()`에서 자동 등록 (blank import로 활성화)
- 새 TxType 추가 시 core 변경 불필요

### 4. 결정론적 상태 루트
- 모든 키 정렬 → length-prefix 인코딩 → SHA-256
- 제안자와 검증자가 동일한 루트 계산 보장

### 5. 이벤트 기반 인덱싱
- Emitter를 통해 비동기 인덱스 갱신
- panic recovery로 노드 크래시 방지

### 6. 원자적 블록 커밋
- 스냅샷 → 실행 → 서명 → 블록체인 추가 → 상태 커밋
- 실패 시 롤백으로 일관성 보장

---

## 보안 기능

| 영역 | 구현 |
|------|------|
| 서명 | ed25519 (표준 라이브러리) |
| 해시 | SHA-256 (결정론적 상태루트, 트랜잭션 ID) |
| P2P 보안 | mTLS (TLS 1.3, 클라이언트 인증서 검증) |
| 리플레이 방지 | 계정별 Nonce + ChainID 검증 |
| 키 저장 | AES-GCM + PBKDF2 (100K iterations) |
| RPC 인증 | Bearer 토큰 (선택적) |
| 입력 검증 | pubkey 형식, 오버플로우, 타임스탬프 윈도우 |
| 네트워크 | 30초 데드라인, 32MB 메시지 제한, 50 피어 제한, 1MB RPC 제한 |

---

## 트랜잭션 처리 흐름

```
사용자 Tx 서명
  → POST /rpc (sendTx)
    → ChainID 검증 → Mempool 추가
      → 합의 루프 (2초)
        → IsProposer? → ProduceBlock
          → 스냅샷 → ExecuteBlock (순차 실행)
            → ComputeRoot → Sign → AddBlock → Commit
              → Broadcast (P2P)
```

---

## 빌드 & 배포

- **Makefile:** `make build`, `make test`, `make vet`, 크로스 컴파일 (darwin-arm64, linux-amd64)
- **Docker:** Alpine 3.19, 멀티스테이지 빌드 (Go 1.22)
- **실행 모드:** `-genkey` (키 생성), `-gencerts` (TLS 인증서), 기본 (풀 노드)
