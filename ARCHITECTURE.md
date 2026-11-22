# 아키텍처

## 구성

```text
게임 서버 ── JSON-RPC sendTx ──▶ rpc ──▶ Mempool
                                           │ 블록 간격마다, 제안자일 때
                                           ▼
피어 ── P2P 블록 ──▶ network        consensus
                  apply_block       produce_block
                        │                 │
                        └────────┬────────┘
                                 ▼
                 vm.Executor ──▶ vm/modules/* 핸들러
                                 │
                                 ▼
                 StateDB (쓰기 버퍼, 스냅샷) ── 커밋 ──▶ LevelDB
                                 │
                                 ▼ 상태 커밋 뒤 이벤트 flush
                 events.Emitter ──▶ indexer, SSE /events, 블록 전파·pruning
```

블록은 두 경로로 들어옵니다. 제안자 차례인 노드는 mempool에서 tx를 골라 produce_block으로 블록을 만들고, 다른 노드는 피어에게 받은 블록을 apply_block으로 적용합니다. 두 흐름 모두 같은 Executor와 StateDB를 거칩니다.

## 코드 지도

의존 방향은 `design/tolchain.yaml`의 계층 foundation → domain → persistence → execution → service를 따릅니다.

| 경로 | 책임 |
|---|---|
| `cmd/node/` | 노드 진입점. 설정·키 읽기, 체인 열기, tip 상태 복구, P2P·RPC·합의 루프 시작, 종료 처리 |
| `cmd/genkey/` | 검증자 키 쌍 생성 |
| `cmd/backup/` | 멈춘 노드의 LevelDB를 읽기 전용으로 열어 상태를 JSON으로 내보내기 |
| `config/` | 설정 읽기와 검증, 제네시스, mTLS 설정 |
| `core/` | Transaction, Block, Blockchain, Mempool, State 인터페이스, 세션 동의 메시지 형식 |
| `crypto/` | ed25519 서명, SHA-256, 주소. `certgen/`은 mTLS 인증서 생성 |
| `storage/` | LevelDB, StateDB(쓰기 버퍼, 스냅샷 스택, 상태 루트), 오래된 블록 pruning |
| `vm/` | Executor(위임 확인, 수수료·nonce, 핸들러 호출, tx별 스냅샷), 핸들러 레지스트리 |
| `vm/modules/` | tx 핸들러 그룹: economy, asset, market, session, inventory, random, delegation, reward |
| `consensus/` | PoA 라운드 로빈, produce_block 단계 본문, 재시작 때 tip 상태 복구 |
| `network/` | TCP P2P(길이 접두사 JSON, mTLS 선택), 피어 관리, 블록 동기화, apply_block 단계 본문 |
| `events/` | 이벤트 Emitter, 블록 실행 중 이벤트를 모으는 Buffer |
| `indexer/` | 이벤트로 갱신하는 보조 인덱스: 소유자별 자산, 플레이어별 세션, 활성 등록, tx 결과 |
| `rpc/` | JSON-RPC 2.0, Bearer 인증, IP별 rate limit, SSE `/events`, `/status`, `/metrics` |
| `metrics/` | Prometheus 지표 정의 |
| `wallet/` | 암호화 키스토어(AES-GCM, PBKDF2), tx 빌더 |
| `design/` | daemok 설계 파일 `tolchain.yaml` |
| `tests/` | 통합 테스트, 벤치마크, golden 파일(`tests/testdata/`) |
| `internal/testutil/` | 테스트용 인메모리 DB |
| `monitoring/`, `scripts/` | Prometheus 설정, Docker 배포 준비 스크립트 `setup.sh` |

`design/tolchain.yaml`은 tx 타입, 이벤트 타입과 payload, 모듈과 의존, 블록 생성·적용 흐름의 기준입니다. `daemok gen`이 여기서 다음 파일을 만듭니다.

```text
design/tolchain.yaml ── daemok gen ──▶ core/tx_type.gen.go
                                       events/*.gen.go
                                       vm/modules/*/register.gen.go   (init에서 핸들러 등록)
                                       cmd/node/modules.gen.go        (모듈 blank import)
                                       consensus/produce_block.gen.go (단계 순서, 되돌리기)
                                       network/apply_block.gen.go
```

## 지켜야 할 규칙

- `*.gen.go`는 손으로 고치지 않습니다. 설계 파일을 고친 뒤 `daemok gen`으로 다시 만들고, `daemok check`로 설계와 import, 생성 파일 최신 여부를 확인합니다.
- produce_block과 apply_block의 단계 순서, 실패 때 되돌리기, 커밋 지점은 생성된 `RunProduceBlock`·`RunApplyBlock`이 정합니다. 손으로 쓰는 `produce_block.go`·`apply_block.go`에는 단계 본문만 둡니다. commit_state가 실패하면 되돌리지 않고 Fatal로 끝냅니다.
- 블록 저장(add_block)이 상태 커밋보다 먼저입니다. 그 사이에 노드가 멈추면 시작할 때 `RecoverTipState`가 tip 블록을 다시 실행합니다.
- 블록 실행 중 이벤트는 Buffer에 모으고 상태 커밋 뒤에만 내보냅니다. 실패한 tx의 이벤트는 버리고 tx_executed만 남깁니다. indexer와 SSE는 커밋된 상태만 봅니다.
- 실행 결과는 모든 노드에서 같아야 합니다. 핸들러는 벽시계 대신 블록 타임스탬프(위임 만료)와 블록 해시(난수)를 씁니다.
- 상태 루트는 등록된 접두사의 키를 정렬해 길이 접두사로 이은 SHA-256입니다. 새 상태 접두사는 `storage/statedb.go`의 `registerPrefix`로만 선언합니다. 그래야 상태 루트와 백업에 들어갑니다.
- 제안자는 실패한 tx를 건너뛰고 블록을 만들지만, 받은 블록은 tx 하나라도 실패하면 거부합니다.
- tx 타입 하나에 핸들러 하나입니다. 같은 타입을 두 번 등록하면 init에서 panic이 납니다.
- `vm/modules/` 아래 모듈끼리는 import하지 않습니다. 예외는 설계 파일에 적힌 reward → asset(자산 스키마 검증 재사용) 하나입니다.
- `tests/testdata/`의 golden 파일은 다른 노드, 디스크, 클라이언트가 의존하는 바이트를 고정합니다. 리팩터링으로 바뀌면 안 되고, 체인 호환을 깨는 의도한 형식 변경일 때만 `-update`로 다시 만듭니다.
- 세션 동의 메시지(`core.SessionConsentMessage`)는 체인 밖 서명자가 같은 바이트를 재현해야 합니다.
