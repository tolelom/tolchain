# TOL Chain

게임(MMORPG, 로그라이크 등) 특화 블록체인. 에셋, 세션, 마켓 등 게임 원시 기능을 온체인에서 직접 처리한다.

## 특징

- **범용 에셋** — 아이템·카드·캐릭터 등을 동일한 구조로 표현. 템플릿으로 스키마를 정의하고 민팅한다.
- **게임 세션** — 플레이어 스테이크를 잠근 뒤 결과에 따라 보상을 분배한다.
- **P2P 마켓** — 에셋을 온체인에서 직접 거래한다.
- **PoA 합의** — 검증자 목록 기반 라운드-로빈 블록 제안. 개발/프라이빗 체인에 적합하다.
- **확장 가능한 VM** — 새 트랜잭션 타입은 `init()`에서 핸들러를 등록하기만 하면 된다.

## 프로젝트 구조

```
tolchain/
├── cmd/node/          # 노드 진입점
├── config/            # 설정 및 제네시스 블록
├── consensus/         # Proof-of-Authority 합의
├── core/              # 트랜잭션·블록·상태 타입 정의
├── crypto/            # SHA-256 해시, ed25519 서명
├── events/            # 블록 이벤트 발행/구독
├── indexer/           # 보조 인덱스 (소유자→에셋, 플레이어→세션)
├── internal/testutil/ # 테스트 전용 인메모리 구현
├── network/           # TCP P2P 네트워킹, 블록 동기화
├── rpc/               # JSON-RPC 2.0 HTTP 서버
├── storage/           # LevelDB 래퍼, StateDB (스냅샷/롤백)
├── tests/             # 통합 테스트
├── vm/                # 트랜잭션 실행기 및 핸들러 레지스트리
│   └── modules/       # asset / economy / market / session 모듈
└── wallet/            # 키 생성·저장, 트랜잭션 서명 헬퍼
```

## 빠른 시작

```bash
# 의존성 설치
go mod download

# 전체 빌드
go build ./...

# 테스트
go test ./...

# 검증자 키 생성
go run ./cmd/node --genkey --key validator.key --password mypassword

# 노드 실행 (기본 설정)
go run ./cmd/node --key validator.key --password mypassword
```

기본 설정으로 실행하면 RPC는 `:8545`, P2P는 `:30303`에서 수신한다.

## 설정

`config.json`이 없으면 기본값으로 실행된다. 생성 예시:

```json
{
  "node_id": "node0",
  "data_dir": "./data",
  "rpc_port": 8545,
  "p2p_port": 30303,
  "max_block_txs": 500,
  "validators": ["<검증자 pubkey hex>"],
  "genesis": {
    "chain_id": "tolchain-dev",
    "alloc": {
      "<pubkey hex>": 1000000
    }
  }
}
```

## RPC API

모든 요청은 `POST /` 에 JSON-RPC 2.0 형식으로 보낸다.

| 메서드 | 파라미터 | 설명 |
|--------|----------|------|
| `getBlockHeight` | — | 현재 블록 높이 |
| `getBlock` | `hash` 또는 `height` | 블록 조회 |
| `getBalance` | `address` | 계정 잔액 |
| `getAsset` | `id` | 에셋 조회 |
| `getSession` | `id` | 세션 조회 |
| `getListing` | `id` | 마켓 리스팅 조회 |
| `getAssetsByOwner` | `owner` | 소유자의 에셋 목록 |
| `sendTx` | 서명된 트랜잭션 | 멤풀에 제출 |
| `getMempoolSize` | — | 멤풀 트랜잭션 수 |

## 트랜잭션 타입

| 타입 | 설명 |
|------|------|
| `transfer` | 토큰 전송 |
| `register_template` | 에셋 템플릿 등록 |
| `mint_asset` | 에셋 민팅 |
| `burn_asset` | 에셋 소각 |
| `transfer_asset` | 에셋 전송 |
| `session_open` | 게임 세션 시작 (스테이크 잠금) |
| `session_result` | 세션 종료 및 보상 분배 |
| `list_market` | 에셋 마켓 등록 |
| `buy_market` | 마켓 구매 |

## 기술 스택

- **언어** — Go 1.22
- **서명** — ed25519 (표준 라이브러리)
- **해시** — SHA-256
- **저장소** — LevelDB (`github.com/syndtr/goleveldb`)
- **RPC** — JSON-RPC 2.0 over HTTP
- **네트워크** — TCP + JSON 인코딩
