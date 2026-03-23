# 지갑 보안 강화 + Delegation 설계

> 작성일: 2026-03-23
> 상태: 승인됨

---

## 개요

현행 커스터디얼 지갑 모델의 보안 취약점(honey pot)을 해소하고, 체인 네이티브 위임(Delegation) 기능을 추가한다.

**결정**: 옵션 B (Phase 1: 보안 강화 + Phase 2: Delegation)

**핵심 원칙**: 게임 내 UX 변화 없음. 현행 커스터디얼 흐름 100% 하위 호환.

---

## Phase 1: 보안 강화 (API 서버)

### 1-1. Per-wallet 키 파생 (HKDF)

**현행 문제**: `WALLET_ENCRYPTION_KEY` 단일 환경변수로 전체 지갑 암호화 → DB + 환경변수 탈취 시 일괄 복호화 가능.

**변경**:
- `encryptPrivKey()` / `decryptPrivKey()`에서 마스터 키를 직접 사용하지 않음
- `HKDF(salt, masterKey, "wallet:" + userID)`로 유저별 파생 키 생성 후 AES-256-GCM 암호화
- HKDF salt: 유저별 16바이트 랜덤 값 생성, `UserWallet.hkdf_salt` 컬럼에 hex 저장

```
기존: AES-GCM(masterKey, privKey)
변경: AES-GCM(HKDF(randomSalt, masterKey, "wallet:" + userID), privKey)
```

**영향 범위**: `a301_server/internal/chain/service.go` — `encryptPrivKey()`, `decryptPrivKey()`, `a301_server/internal/chain/model.go` — `HKDFSalt` 컬럼 추가

### 1-2. 키 내보내기 API

- `POST /api/chain/wallet/export` — JWT 인증 + 비밀번호 재확인 (body: `{"password": "..."}`)
- 응답: 개인키 hex (1회 표시, 서버 로그에 남기지 않음)
- Rate limit: 사용자당 1회/5분
- 감사 로그: 내보내기 요청 시 slog.Warn으로 userID, IP, 시각 기록 (키 값은 기록하지 않음)

**영향 범위**: `a301_server/internal/chain/handler.go` 엔드포인트 추가, `chain/service.go`에 `ExportPrivKey(userID, password)` 메서드

### 1-3. 마이그레이션

**방식**: 서버 시작 시 일괄 마이그레이션 (방식 A)

- `UserWallet` 테이블에 `key_version` 컬럼 추가 (기본값 1, 신규 2)
- 서버 시작 시 `key_version=1`인 지갑을 조회 → 구 방식 복호화 → 신 방식 재암호화 → DB 업데이트
- 레코드 단위 처리: 개별 지갑마다 복호화→재암호화→UPDATE. 실패 시 해당 지갑만 에러 로그 남기고 계속 진행 (부분 마이그레이션 허용). 미마이그레이션 지갑은 `key_version=1`로 남아 구 방식 복호화로 폴백.
- 마이그레이션 완료 후 정상 서빙 시작

**영향 범위**: `a301_server/internal/chain/model.go` (컬럼 추가), `a301_server/internal/chain/service.go` (마이그레이션 로직)

---

## Phase 2: Delegation (체인 네이티브 위임)

### 2-1. Transaction 구조 확장

```go
type Transaction struct {
    // ... 기존 필드
    OnBehalfOf string `json:"on_behalf_of,omitempty"` // 위임자 pubkey hex
}
```

- `OnBehalfOf` 비어있음 → 기존과 동일 (From이 실행 주체)
- `OnBehalfOf` 있음 → From이 서명자(grantee), OnBehalfOf가 실제 실행 주체(granter)
- 서명 해시 계산: `signingBody`에 `OnBehalfOf` 필드 추가. `omitempty` JSON 태그로 빈 문자열일 때는 직렬화에서 제외 → 기존 TX(`OnBehalfOf == ""`)의 해시는 변경되지 않음 (하위 호환)

### 2-2. 새 TxType 2개

**`grant_delegation`** — 위임 생성

```go
type GrantDelegationPayload struct {
    Grantee    string   `json:"grantee"`       // 위임받는 자 pubkey hex
    AllowTypes []string `json:"allow_types"`    // 허용 TxType 목록
    ExpiresAt  int64    `json:"expires_at"`     // Unix timestamp (초), 0 = 무제한
    MaxUses    uint64   `json:"max_uses"`       // 0 = 무제한
    MaxAmount  uint64   `json:"max_amount"`     // 토큰 지출 한도, 0 = 무제한
}
```

**`revoke_delegation`** — 위임 취소 (Granter만 실행 가능)

```go
type RevokeDelegationPayload struct {
    Grantee string `json:"grantee"` // 취소 대상 grantee pubkey
}
```

### 2-3. StateDB 저장

```
키 패턴: deleg:{granter_pubkey}:{grantee_pubkey}
값:      DelegationGrant JSON
```

```go
type DelegationGrant struct {
    Granter    string   `json:"granter"`
    Grantee    string   `json:"grantee"`
    AllowTypes []string `json:"allow_types"`
    ExpiresAt  int64    `json:"expires_at"`
    MaxUses    uint64   `json:"max_uses"`
    UsedCount  uint64   `json:"used_count"`
    MaxAmount  uint64   `json:"max_amount"`
    SpentAmount uint64  `json:"spent_amount"`
    CreatedAt  int64    `json:"created_at"`
}
```

### 2-4. Executor 검증 흐름 변경

```
TX 수신
  ├─ OnBehalfOf 없음 → 기존 흐름 (From 서명 검증 → From 계정 실행)
  └─ OnBehalfOf 있음 →
       1. From(grantee) 서명 검증
       2. deleg:{OnBehalfOf}:{From} grant 조회
       3. grant 유효성 검증:
          - 만료 시각 (expires_at) 초과?
          - 사용 횟수 (used_count >= max_uses)?
          - TxType이 allow_types에 포함?
          - TxType이 `grant_delegation` 또는 `revoke_delegation`이면 거부 (재위임 방지)
          - 토큰 지출이 max_amount 초과?
       4. 통과 → OnBehalfOf(granter) 계정의 nonce/balance로 실행
       5. used_count++, spent_amount 갱신

**Nonce 정책**: Delegation TX는 **Granter(OnBehalfOf)의 nonce**를 사용한다. Grantee는 TX 생성 전 `getBalance` RPC로 Granter의 현재 nonce를 조회해야 한다. 동일 Granter에 대해 여러 Grantee가 동시에 TX를 보내면 nonce 충돌이 발생할 수 있으나, mempool이 nonce 순서대로 처리하므로 충돌 TX는 거부되고 재시도하면 된다. 이는 일반 계정에서 동시 TX를 보내는 것과 동일한 동작이다.

**Fee 정책**: Delegation TX의 fee는 **Granter(OnBehalfOf) 계정**에서 차감한다. Grantee는 잔액 없이도 위임 TX를 실행할 수 있다.

**SpentAmount 추적**: `max_amount`는 Executor 레벨에서 추적한다. transfer의 amount, buy_market의 listing price 등 각 VM 모듈이 반환하는 `TokenSpent uint64` 값을 Executor가 받아서 `spent_amount`에 누적한다. VM 핸들러 인터페이스에 `TokenSpent` 반환 필드를 추가한다.

**만료 Grant 정리**: 블록 생성 시 별도 GC를 하지 않는다. Executor가 검증 시점에 만료를 감지하면 해당 Grant를 삭제한다 (lazy cleanup).
```

### 2-5. VM 모듈

- 새 `vm/modules/delegation/` 모듈 생성
- `grant_delegation`, `revoke_delegation` 핸들러 등록
- 기존 모듈 패턴과 동일하게 `init()` 블랭크 임포트로 자동 등록

### 2-6. wallet 패키지 확장

`tolchain/wallet/wallet.go`에 Delegation TX 빌더 추가:
- `GrantDelegation()` — 위임 생성 TX
- `RevokeDelegation()` — 위임 취소 TX
- `NewDelegatedTx()` — OnBehalfOf 필드가 포함된 위임 TX 빌더

### 2-7. RPC API 확장

조회:
- `getDelegation` — 특정 granter→grantee 위임 조회
- `getDelegationsByGranter` — granter의 모든 위임 목록
- `getDelegationsByGrantee` — grantee가 받은 모든 위임 목록

### 2-8. API 서버 변경

현행 커스터디얼 흐름은 변경 없음. Delegation은 자기 키를 가진 유저가 직접 체인 RPC로 `grant_delegation` TX를 보내는 방식이므로, API 서버에 별도 엔드포인트 불필요.

**Phase 의존성**: 유저가 Delegation을 사용하려면 먼저 Phase 1의 키 내보내기로 자기 개인키를 확보해야 한다. Phase 1이 선행 조건.

### 2-9. 이벤트

기존 이벤트 시스템에 추가:
- `delegation_granted` — 위임 생성 시
- `delegation_revoked` — 위임 취소 시

---

## 변경 파일 요약

### Phase 1 (API 서버: `a301_server/`)
| 파일 | 변경 |
|------|------|
| `internal/chain/service.go` | HKDF 키 파생, 마이그레이션 로직, ExportPrivKey() |
| `internal/chain/handler.go` | 키 내보내기 엔드포인트 |
| `internal/chain/model.go` | UserWallet에 key_version, hkdf_salt 컬럼 추가 |
| `routes/routes.go` | 라우트 등록 |
| `go.mod` | golang.org/x/crypto (HKDF) — 이미 의존성에 포함 |

### Phase 2 (체인: `tolchain/`)
| 파일 | 변경 |
|------|------|
| `core/transaction.go` | OnBehalfOf 필드, 서명 해시에 포함 |
| `core/types.go` | TxType 상수 2개, Payload 구조체 2개, DelegationGrant 구조체 |
| `vm/executor.go` | OnBehalfOf 검증 로직 |
| `vm/modules/delegation/delegation.go` | grant/revoke 핸들러 |
| `wallet/wallet.go` | GrantDelegation(), RevokeDelegation(), NewDelegatedTx() |
| `rpc/server.go` | getDelegation, getDelegationsByGranter |
| `events/types.go` | 이벤트 타입 2개 추가 |
| `indexer/indexer.go` | delegation 인덱스 (선택) |

---

## 테스트 전략

### Phase 1
- `service_test.go`: HKDF 키 파생 → 암호화/복호화 라운드트립
- `service_test.go`: 마이그레이션 (v1→v2 변환 검증, 실패 시 폴백 검증)
- `handler_test.go`: 키 내보내기 API (비밀번호 검증, rate limit)

### Phase 2
- `vm/modules/delegation/delegation_test.go`: grant/revoke 핸들러
- `vm/executor_test.go`: OnBehalfOf 검증 흐름 (정상, 만료, 횟수 초과, 재위임 거부, 지출 한도)
- `wallet/wallet_test.go`: Delegation TX 빌더
- `core/transaction_test.go`: OnBehalfOf 서명 해시 하위 호환성
- `tests/`: Delegation 통합 테스트 (grant → delegated tx → revoke → 실행 실패)

---

## 비고

- Phase 1이 Phase 2의 선행 조건 (키 내보내기 → Delegation 사용 가능)
- Phase 2 배포 전에도 기존 시스템은 정상 동작
- Delegation 위에 나중에 세션키/외부 지갑 연동을 추가할 수 있음
