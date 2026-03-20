# TOL Chain 지갑 아키텍처 설계

> 작성일: 2026-03-20
> 상태: 설계 검토 중 (구현 전)

---

## 1. 현행 구조

### 커스터디얼 모델 (a301_server)

```
사용자 → API 서버 (JWT 인증)
  → DB에서 암호화된 개인키 로드 (AES-256-GCM)
  → wallet.New(privKey)로 트랜잭션 서명
  → 체인에 제출
```

- 회원가입 시 서버가 Ed25519 키페어 자동 생성
- 개인키는 `WALLET_ENCRYPTION_KEY` (환경변수, 32바이트) 하나로 전체 암호화
- MySQL `user_wallets` 테이블에 `encrypted_priv_key` + `enc_nonce` 저장
- 사용자는 개인키에 접근할 방법 없음 (내보내기 API 없음)
- 모든 트랜잭션을 서버가 대리 서명

### 현행 모델의 보안 위험

- **Honey pot**: 환경변수 1개 + DB 탈취 시 전체 지갑 일괄 복호화 가능
- 환경변수는 컨테이너 이미지, CI 로그, 프로세스 메모리 덤프에서 노출 가능
- 사용자 자산 소유권 없음 — 서버 신뢰 필수

---

## 2. 업계 리서치

### 블록체인 게임별 지갑 모델

| 게임 | 모델 | 특징 |
|---|---|---|
| **Big Time** | 커스터디얼 + 출금 옵션 | 게임 중 마찰 제로, 원할 때 외부 지갑으로 출금 |
| **Axie Infinity** | MPC 임베디드 (Mavis ID) | 소셜 로그인, Shamir 키 분할, 서버 단독 사용 불가 |
| **Gods Unchained** | 임베디드 (Immutable Passport) | 이메일 로그인, ZK-rollup 배치 처리 |
| **Pixels** | MPC 임베디드 (Ronin Mavis ID) | 플레이어가 블록체인 존재 자체를 모름 |
| **Star Atlas** | 완전 논커스터디얼 (Phantom) | 매번 서명 팝업 → UX 최악, 진입장벽 높음 |
| **Starknet 게임들** | 세션키 (Cartridge) | AA 네이티브, 로그인 시 1회 승인 후 자동 |

### 업계 트렌드 (2024~2026)

- "MetaMask 연결" 시대는 종료 — **보이지 않는 블록체인**이 표준
- 소셜 로그인 + 임베디드/MPC 지갑이 주류
- 게임 로직은 오프체인, 경제 이벤트만 온체인
- 서버 오퍼레이터가 민팅/보상 처리
- 가스비는 게임이 대납
- 자기보관은 **옵션으로 제공** (키 내보내기)

### 핵심 인사이트

> "not your keys, not your crypto"는 지나친 단순화 (a16z 리서치).
> Non-custodial 지갑도 다수의 custodial 접점 존재.
> 중요한 건 이분법이 아니라 키 생성/저장/사용 각 단계별 보안.

---

## 3. 검토한 옵션

### 옵션 1: 현행 유지 + 보안 강화 + 키 내보내기

**변경 범위**: API 서버만
**작업량**: 1~2일
**체인 변경**: 없음

- `WALLET_ENCRYPTION_KEY`를 AWS KMS / HashiCorp Vault로 이전
- Per-wallet 키 파생: `HKDF(master, userID)` → DB 침해 시 일괄 복호화 방지
- 키 내보내기 API: `GET /api/chain/wallet/export` (JWT + 비밀번호 재확인)

### 옵션 2: Delegation 네이티브 지원 (Cosmos x/authz 방식)

**변경 범위**: 체인 + API 서버
**작업량**: 2~3일
**체인 변경**: TxType 2개 + StateDB 패턴 1개

#### 개념

```
1. 유저가 자기 키로 grant_delegation TX 전송
   → "서버 공개키 XXX에게 transfer, equip_item 권한을 24시간 위임"
2. 체인이 StateDB에 grant 기록: deleg:{granter}:{grantee} → 정책 JSON
3. 서버가 exec_delegated TX 전송 (OnBehalfOf 필드에 유저 공개키)
4. 체인이 grant 검증 → 유저 계정의 nonce/balance에서 실행
5. 만료 시 자동 무효화, revoke_delegation으로 즉시 취소 가능
```

#### 체인 변경 상세

```go
// 1. Transaction 구조 확장
type Transaction struct {
    // ... 기존 필드
    OnBehalfOf string `json:"on_behalf_of,omitempty"` // 위임자 pubkey hex
}

// 2. 새 TxType
const (
    TxGrantDelegation  TxType = "grant_delegation"
    TxRevokeDelegation TxType = "revoke_delegation"
)

// 3. 새 Payload
type GrantDelegationPayload struct {
    Grantee    string   `json:"grantee"`     // 위임받는 자 pubkey hex
    AllowTypes []string `json:"allow_types"` // 허용 TxType 목록
    ExpiresAt  int64    `json:"expires_at"`  // Unix timestamp (나노초)
    MaxUses    uint64   `json:"max_uses"`    // 0 = 무제한
    MaxAmount  uint64   `json:"max_amount"`  // 토큰 지출 한도, 0 = 무제한
}

// 4. StateDB 키 패턴
// deleg:{granter_pubkey}:{grantee_pubkey} → DelegationGrant JSON

// 5. Executor 검증 로직 수정
// - OnBehalfOf가 있으면: tx.From 서명 검증 + delegation grant 존재/유효 확인
// - grant 범위 내 TxType인지 확인
// - 유저(granter) 계정의 nonce/balance에서 실행
// - max_uses 차감, 소진 시 grant 삭제
```

### 옵션 3: 세션키 전체 구현

**변경 범위**: 체인 + API 서버 + 클라이언트
**작업량**: 1~2주
**체인 변경**: 검증 로직 + 세션키 state

옵션 2에 추가로:
- 세션키 전용 state: `sesskey:{owner}:{session_pubkey}`
- 세션별 정책: 허용 TxType, 만료시각, 최대 사용횟수, 토큰 지출 한도
- 세션 키로 서명된 TX의 별도 검증 경로
- 클라이언트(Unity/런처)에서 세션 키 생성 + 서명

### 옵션 4: 클라이언트 키 보관 (풀 논커스터디얼)

**변경 범위**: 런처 + 게임 클라이언트 + API 서버 전면 변경
**작업량**: 2~3주
**체인 변경**: 없음 (릴레이만 추가)

- 키를 클라이언트에서 생성 + 로컬 저장 (OS keychain 또는 암호화 파일)
- 서버는 공개키만 보유, 서명된 TX를 릴레이
- 키 분실 = 자산 영구 소실

---

## 4. 비교표

| | 옵션 1 | 옵션 2 | 옵션 3 | 옵션 4 |
|---|---|---|---|---|
| **작업량** | 1~2일 | 2~3일 | 1~2주 | 2~3주 |
| **게임 UX 변화** | 없음 | 없음 | 없음 | 없음 |
| **자기보관** | 키 내보내기만 | 외부 키 사용 가능 | 세션 단위 | 완전 |
| **DB 침해 시** | 전체 탈취 가능 (KMS로 완화) | 동일 | 세션키만 노출 | 지갑 안전 |
| **키 분실 시** | 서버 복구 가능 | 서버 복구 가능 | 서버 복구 가능 | **영구 소실** |
| **외부 지갑 연동** | 불가 | **가능** | 가능 | 가능 |
| **하위 호환** | 완전 | 완전 | 완전 | 불가 (전면 전환) |
| **확장성** | 한계 있음 | 좋음 | 좋음 | 좋음 |

---

## 5. 결정 방향

### 추천: 옵션 1 + 옵션 2 동시 진행 (3~4일)

**이유:**
1. 체인 변경이 최소 (TxType 2개 + state 1개)
2. 현재 커스터디얼 모드가 그대로 동작 (하위 호환)
3. Delegation 위에 나중에 세션키나 외부 지갑 연동을 얹을 수 있음
4. 보안 강화(KMS, per-wallet 키)로 현행 honey pot 위험 해소
5. 키 내보내기로 "자산 소유권" 최소 요건 충족

### 구현 순서 (안)

```
Phase 1 — 보안 강화 (옵션 1)
  ├── WALLET_ENCRYPTION_KEY → KMS/Vault 이전
  ├── Per-wallet 키 파생 (HKDF)
  └── 키 내보내기 API

Phase 2 — Delegation (옵션 2)
  ├── 체인: grant_delegation / revoke_delegation TxType
  ├── 체인: DelegationGrant state + 검증 로직
  ├── 체인: Transaction.OnBehalfOf 필드
  ├── wallet 패키지: Delegation TX 빌더
  └── API 서버: Delegation 관리 엔드포인트

Phase 3 — 지갑 UI
  ├── 웹 지갑 페이지 (a301_client)
  │   ├── 잔액 조회
  │   ├── 토큰 전송
  │   ├── NFT 인벤토리
  │   ├── 마켓 (리스팅/구매/취소)
  │   ├── 트랜잭션 내역
  │   └── 키 내보내기
  └── 게임 내 지갑 UI (Unity)
      ├── 로비 지갑 탭
      ├── 잔액, 보유 NFT, 장착 장비
      └── 간단한 전송 기능
```

---

## 6. 참고 자료

### 세션키 & Account Abstraction
- [Session Keys on Starknet](https://www.starknet.io/blog/session-keys-on-starknet-unlocking-gasless-secure-transactions/)
- [Openfort: Technical Dive Session Keys](https://www.openfort.io/blog/technical-dive-session-keys)
- [Biconomy Modular Session Keys](https://www.biconomy.io/post/modular-session-keys)
- [SNIP Session Keys for Smart Accounts](https://community.starknet.io/t/snip-session-keys-for-smart-accounts/116132)
- [Cartridge Controller (StarkNet Gaming)](https://docs.cartridge.gg/controller/getting-started)

### Embedded Wallet 프로바이더
- [Privy: How Embedded Wallets Work](https://privy.io/blog/how-privy-embedded-wallets-work) — Shamir + TEE
- [Sequence: Embedded Wallet Architecture](https://docs.sequence.xyz/solutions/wallets/embedded-wallet/architecture/overview/) — 2/2 멀티시그
- [Turnkey: Wallet Provider Key Management](https://www.turnkey.com/blog/wallet-provider-key-management) — TEE-native
- [Dfns: How MPC Wallets Work](https://docs.dfns.co/core-concepts/how-mpc-wallets-work) — 5/3 TSS
- [Immutable Passport](https://www.immutable.com/blog/under-the-hood-immutable-passport) — KMS + Guardian

### Delegation
- [Cosmos SDK x/authz 모듈](https://docs.cosmos.network/main/modules/authz) — 네이티브 위임의 참조 구현

### 보안
- [a16z: Wallet Security — The Non-Custodial Fallacy](https://a16zcrypto.com/posts/article/wallet-security-non-custodial-fallacy/)
- [Axie/Ronin $625M 해킹 사후 분석](https://www.theblock.co/post/156038/how-a-fake-job-offer-took-down-the-worlds-most-popular-crypto-game)
  - 원인: 위임 권한 화이트리스트 해제 안 함 → **위임 자동 만료 필수**

### 게임 아키텍처
- [thirdweb: Blockchain Game Architecture](https://blog.thirdweb.com/blockchain-game-architecture/)
