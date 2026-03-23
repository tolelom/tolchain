# 지갑 보안 강화 + Delegation 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per-wallet HKDF 키 파생으로 honey pot 해소, 키 내보내기 API 추가, 체인 네이티브 Delegation 지원

**Architecture:** Phase 1은 API 서버(`a301_server`)만 변경 — HKDF 키 파생 + 마이그레이션 + 키 내보내기. Phase 2는 체인(`tolchain`)에 Delegation TxType 2개 + Executor 검증 로직 + VM 모듈 추가.

**Tech Stack:** Go, Fiber v2, GORM/MySQL, AES-256-GCM, HKDF (golang.org/x/crypto/hkdf), tolchain (custom PoA blockchain)

**Spec:** `tolchain/docs/superpowers/specs/2026-03-23-wallet-security-delegation-design.md`

**Go binary:** `C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe` (bash에서 `go` 직접 호출 불가)

**MaxAmount 스코프 결정:** `MaxAmount`/`SpentAmount` 추적은 VM Handler 인터페이스 변경이 필요하여 범위가 큼. 1차에서는 `UsedCount` + `AllowTypes` + `ExpiresAt`으로만 제한. `MaxAmount` 필드는 구조체에 포함하되 검증 로직은 향후 추가 (TODO 주석).

---

## Phase 1: 보안 강화 (API 서버)

### Task 1: UserWallet 모델에 key_version, hkdf_salt 컬럼 추가

**Files:**
- Modify: `a301_server/internal/chain/model.go`

- [ ] **Step 1: UserWallet 구조체에 필드 추가**

`a301_server/internal/chain/model.go`의 `UserWallet` 구조체에 2개 필드 추가:

```go
type UserWallet struct {
	ID               uint           `json:"id"        gorm:"primaryKey"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	DeletedAt        gorm.DeletedAt `json:"-"         gorm:"index"`
	UserID           uint           `json:"userId"    gorm:"uniqueIndex;not null"`
	PubKeyHex        string         `json:"pubKeyHex" gorm:"type:varchar(64);uniqueIndex;not null"`
	Address          string         `json:"address"   gorm:"type:varchar(40);uniqueIndex;not null"`
	EncryptedPrivKey string         `json:"-"         gorm:"type:varchar(512);not null"`
	EncNonce         string         `json:"-"         gorm:"type:varchar(48);not null"`
	KeyVersion       int            `json:"-"         gorm:"type:tinyint;default:1;not null"`
	HKDFSalt         string         `json:"-"         gorm:"type:varchar(32)"` // 16 bytes hex, nullable for v1
}
```

- [ ] **Step 2: 빌드 확인**

Run: `cd a301_server && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe build ./...`
Expected: 성공

- [ ] **Step 3: 커밋**

```bash
cd a301_server && git add internal/chain/model.go && git commit -m "feat: add key_version and hkdf_salt columns to UserWallet"
```

---

### Task 2: HKDF per-wallet 키 파생 구현

**Files:**
- Modify: `a301_server/internal/chain/service.go`

- [ ] **Step 1: import 추가 및 derivePerWalletKey 함수 작성**

`a301_server/internal/chain/service.go`에 import 추가: `"crypto/sha256"`, `"strconv"`, `"golang.org/x/crypto/hkdf"`

기존 `encryptPrivKey` 위에 새 함수:

```go
func (s *Service) derivePerWalletKey(salt []byte, userID uint) ([]byte, error) {
	info := []byte("wallet:" + strconv.FormatUint(uint64(userID), 10))
	r := hkdf.New(sha256.New, s.encKeyBytes, salt, info)
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("HKDF key derivation failed: %w", err)
	}
	return key, nil
}
```

- [ ] **Step 2: encryptPrivKeyV2 / decryptPrivKeyV2 작성**

기존 v1 함수는 유지 (마이그레이션용). 그 아래에 v2 함수 추가:

```go
func (s *Service) encryptPrivKeyV2(privKey tocrypto.PrivateKey, userID uint) (cipherHex, nonceHex, saltHex string, err error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", "", "", err
	}
	key, err := s.derivePerWalletKey(salt, userID)
	if err != nil {
		return "", "", "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", "", err
	}
	cipherText := gcm.Seal(nil, nonce, []byte(privKey), nil)
	return hex.EncodeToString(cipherText), hex.EncodeToString(nonce), hex.EncodeToString(salt), nil
}

func (s *Service) decryptPrivKeyV2(cipherHex, nonceHex, saltHex string, userID uint) (tocrypto.PrivateKey, error) {
	cipherText, err := hex.DecodeString(cipherHex)
	if err != nil {
		return nil, err
	}
	nonce, err := hex.DecodeString(nonceHex)
	if err != nil {
		return nil, err
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return nil, err
	}
	key, err := s.derivePerWalletKey(salt, userID)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return nil, fmt.Errorf("wallet decryption failed: %w", err)
	}
	return tocrypto.PrivateKey(plaintext), nil
}
```

- [ ] **Step 3: CreateWallet을 v2로 전환**

```go
func (s *Service) CreateWallet(userID uint) (*UserWallet, error) {
	w, err := wallet.Generate()
	if err != nil {
		return nil, fmt.Errorf("key generation failed: %w", err)
	}
	cipherHex, nonceHex, saltHex, err := s.encryptPrivKeyV2(w.PrivKey(), userID)
	if err != nil {
		return nil, fmt.Errorf("key encryption failed: %w", err)
	}
	uw := &UserWallet{
		UserID:           userID,
		PubKeyHex:        w.PubKey(),
		Address:          w.Address(),
		EncryptedPrivKey: cipherHex,
		EncNonce:         nonceHex,
		KeyVersion:       2,
		HKDFSalt:         saltHex,
	}
	if err := s.repo.Create(uw); err != nil {
		return nil, fmt.Errorf("wallet save failed: %w", err)
	}
	return uw, nil
}
```

- [ ] **Step 4: loadUserWallet에 v1/v2 분기 추가**

```go
func (s *Service) loadUserWallet(userID uint) (*wallet.Wallet, string, error) {
	uw, err := s.repo.FindByUserID(userID)
	if err != nil {
		return nil, "", fmt.Errorf("wallet not found: %w", err)
	}
	var privKey tocrypto.PrivateKey
	if uw.KeyVersion >= 2 {
		privKey, err = s.decryptPrivKeyV2(uw.EncryptedPrivKey, uw.EncNonce, uw.HKDFSalt, uw.UserID)
	} else {
		privKey, err = s.decryptPrivKey(uw.EncryptedPrivKey, uw.EncNonce)
	}
	if err != nil {
		log.Printf("WARNING: wallet decryption failed for userID=%d: %v", userID, err)
		return nil, "", fmt.Errorf("wallet decryption failed")
	}
	return wallet.New(privKey), uw.PubKeyHex, nil
}
```

- [ ] **Step 5: 빌드 확인**

Run: `cd a301_server && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe build ./...`

- [ ] **Step 6: 커밋**

```bash
cd a301_server && git add internal/chain/service.go && git commit -m "feat: HKDF per-wallet key derivation for wallet encryption"
```

---

### Task 3: Phase 1 단위 테스트

**Files:**
- Create: `a301_server/internal/chain/service_encryption_test.go`

- [ ] **Step 1: HKDF 라운드트립 테스트 작성**

```go
package chain

import (
	"testing"

	tocrypto "github.com/tolelom/tolchain/crypto"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	// 32-byte test key
	encKey := make([]byte, 32)
	for i := range encKey {
		encKey[i] = byte(i)
	}
	return &Service{encKeyBytes: encKey}
}

func TestEncryptDecryptV2_Roundtrip(t *testing.T) {
	s := newTestService(t)
	priv, _, err := tocrypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	cipherHex, nonceHex, saltHex, err := s.encryptPrivKeyV2(priv, 42)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.decryptPrivKeyV2(cipherHex, nonceHex, saltHex, 42)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hex() != priv.Hex() {
		t.Errorf("roundtrip mismatch: got %s, want %s", got.Hex(), priv.Hex())
	}
}

func TestDecryptV2_WrongUserID_Fails(t *testing.T) {
	s := newTestService(t)
	priv, _, _ := tocrypto.GenerateKeyPair()
	cipherHex, nonceHex, saltHex, _ := s.encryptPrivKeyV2(priv, 42)
	_, err := s.decryptPrivKeyV2(cipherHex, nonceHex, saltHex, 99) // wrong userID
	if err == nil {
		t.Error("expected error for wrong userID")
	}
}

func TestV1V2_DifferentCiphertext(t *testing.T) {
	s := newTestService(t)
	priv, _, _ := tocrypto.GenerateKeyPair()
	v1cipher, _, _ := s.encryptPrivKey(priv)
	v2cipher, _, _, _ := s.encryptPrivKeyV2(priv, 1)
	if v1cipher == v2cipher {
		t.Error("v1 and v2 should produce different ciphertext")
	}
}
```

- [ ] **Step 2: 테스트 실행**

Run: `cd a301_server && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe test ./internal/chain/... -run TestEncrypt -v`
Expected: PASS

- [ ] **Step 3: 커밋**

```bash
cd a301_server && git add internal/chain/service_encryption_test.go && git commit -m "test: HKDF per-wallet encryption unit tests"
```

---

### Task 4: 서버 시작 시 일괄 마이그레이션

**Files:**
- Modify: `a301_server/internal/chain/repository.go`
- Modify: `a301_server/internal/chain/service.go`
- Modify: `a301_server/main.go`

- [ ] **Step 1: Repository에 마이그레이션용 메서드 추가**

`a301_server/internal/chain/repository.go`에 추가:

```go
func (r *Repository) FindAllByKeyVersion(version int) ([]UserWallet, error) {
	var wallets []UserWallet
	if err := r.db.Where("key_version = ?", version).Find(&wallets).Error; err != nil {
		return nil, err
	}
	return wallets, nil
}

func (r *Repository) UpdateEncryption(id uint, encPrivKey, encNonce, hkdfSalt string, keyVersion int) error {
	return r.db.Model(&UserWallet{}).Where("id = ?", id).Updates(map[string]any{
		"encrypted_priv_key": encPrivKey,
		"enc_nonce":          encNonce,
		"hkdf_salt":          hkdfSalt,
		"key_version":        keyVersion,
	}).Error
}
```

- [ ] **Step 2: Service에 MigrateWalletKeys 메서드 추가**

```go
func (s *Service) MigrateWalletKeys() error {
	wallets, err := s.repo.FindAllByKeyVersion(1)
	if err != nil {
		return fmt.Errorf("query v1 wallets: %w", err)
	}
	if len(wallets) == 0 {
		return nil
	}
	log.Printf("INFO: migrating %d v1 wallets to v2 (HKDF)", len(wallets))
	var migrated, failed int
	for _, uw := range wallets {
		privKey, err := s.decryptPrivKey(uw.EncryptedPrivKey, uw.EncNonce)
		if err != nil {
			log.Printf("ERROR: v1 decrypt failed for walletID=%d userID=%d: %v", uw.ID, uw.UserID, err)
			failed++
			continue
		}
		cipherHex, nonceHex, saltHex, err := s.encryptPrivKeyV2(privKey, uw.UserID)
		if err != nil {
			log.Printf("ERROR: v2 encrypt failed for walletID=%d userID=%d: %v", uw.ID, uw.UserID, err)
			failed++
			continue
		}
		if err := s.repo.UpdateEncryption(uw.ID, cipherHex, nonceHex, saltHex, 2); err != nil {
			log.Printf("ERROR: DB update failed for walletID=%d userID=%d: %v", uw.ID, uw.UserID, err)
			failed++
			continue
		}
		migrated++
	}
	log.Printf("INFO: wallet migration complete: %d migrated, %d failed", migrated, failed)
	return nil
}
```

- [ ] **Step 3: main.go에서 마이그레이션 호출**

`chainSvc` 생성 직후, 라우트 등록 전에:

```go
if err := chainSvc.MigrateWalletKeys(); err != nil {
	log.Fatalf("wallet key migration failed: %v", err)
}
```

- [ ] **Step 4: 빌드 확인**

Run: `cd a301_server && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe build ./...`

- [ ] **Step 5: 커밋**

```bash
cd a301_server && git add internal/chain/repository.go internal/chain/service.go main.go && git commit -m "feat: v1→v2 wallet key migration on server startup"
```

---

### Task 5: 키 내보내기 API

**Files:**
- Modify: `a301_server/internal/auth/service.go`
- Modify: `a301_server/internal/chain/service.go`
- Modify: `a301_server/internal/chain/handler.go`
- Modify: `a301_server/routes/routes.go`
- Modify: `a301_server/main.go`

- [ ] **Step 1: auth Service에 VerifyPassword 메서드 추가**

`a301_server/internal/auth/service.go`에:

```go
func (s *Service) VerifyPassword(userID uint, password string) error {
	user, err := s.repo.FindByID(userID)
	if err != nil {
		return fmt.Errorf("user not found")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return fmt.Errorf("invalid password")
	}
	return nil
}
```

- [ ] **Step 2: chain Service에 passwordVerifier + ExportPrivKey**

Service 구조체에 필드 추가: `passwordVerifier func(userID uint, password string) error`

```go
func (s *Service) SetPasswordVerifier(fn func(userID uint, password string) error) {
	s.passwordVerifier = fn
}

func (s *Service) ExportPrivKey(userID uint, password string) (string, error) {
	if s.passwordVerifier == nil {
		return "", fmt.Errorf("password verifier not configured")
	}
	if err := s.passwordVerifier(userID, password); err != nil {
		return "", err
	}
	w, _, err := s.loadUserWallet(userID)
	if err != nil {
		return "", err
	}
	return w.PrivKey().Hex(), nil
}
```

- [ ] **Step 3: Handler에 ExportWallet 엔드포인트 + rate limit**

`a301_server/internal/chain/handler.go`에 import `"log/slog"` 추가. Handler 구조체에 rate limit 맵 추가:

```go
type exportRequest struct {
	Password string `json:"password"`
}

func (h *Handler) ExportWallet(c *fiber.Ctx) error {
	userID := c.Locals("userID").(uint)
	var req exportRequest
	if err := c.BodyParser(&req); err != nil || req.Password == "" {
		return c.Status(400).JSON(fiber.Map{"error": "password is required"})
	}
	slog.Warn("wallet export requested", "userID", userID, "ip", c.IP())
	privKeyHex, err := h.svc.ExportPrivKey(userID, req.Password)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "invalid password"})
	}
	return c.JSON(fiber.Map{"privateKey": privKeyHex})
}
```

> **구현 에이전트에게**: rate limit은 기존 프로젝트의 rate limiter 패턴을 확인하고, 사용자당 1회/5분 제한을 미들웨어 또는 핸들러 내 로직으로 추가할 것. Redis 기반이면 `export:{userID}` 키로 TTL 5분 설정.

- [ ] **Step 4: routes.go에 라우트 등록**

`a301_server/routes/routes.go`의 chain 조회 그룹에:

```go
chainR.Post("/wallet/export", chainHandler.ExportWallet)
```

- [ ] **Step 5: main.go에서 콜백 연결**

```go
chainSvc.SetPasswordVerifier(authSvc.VerifyPassword)
```

- [ ] **Step 6: 빌드 확인**

Run: `cd a301_server && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe build ./...`

- [ ] **Step 7: 커밋**

```bash
cd a301_server && git add internal/auth/service.go internal/chain/handler.go internal/chain/service.go routes/routes.go main.go && git commit -m "feat: wallet private key export API with password verification"
```

---

## Phase 2: Delegation (체인)

### Task 6: Transaction 구조 확장 — OnBehalfOf 필드

**Files:**
- Modify: `tolchain/core/transaction.go`
- Modify: `tolchain/core/transaction_test.go`

- [ ] **Step 1: Transaction 및 signingBody에 OnBehalfOf 추가**

`Transaction` 구조체 (L38-48)에 `OnBehalfOf string \`json:"on_behalf_of,omitempty"\`` 추가.
`signingBody` 구조체 (L51-59)에 동일 필드 추가.
`Hash()` 내 `signingBody` 리터럴에 `OnBehalfOf: tx.OnBehalfOf` 추가.

- [ ] **Step 2: 하위 호환성 테스트 작성**

`tolchain/core/transaction_test.go`에:

```go
func TestHash_OnBehalfOf_BackwardCompatible(t *testing.T) {
	tx1, _ := NewTransaction("chain", TxTransfer, "aabb", 0, 0, TransferPayload{To: "ccdd", Amount: 100})
	tx2, _ := NewTransaction("chain", TxTransfer, "aabb", 0, 0, TransferPayload{To: "ccdd", Amount: 100})
	tx2.OnBehalfOf = ""
	if tx1.Hash() != tx2.Hash() {
		t.Error("empty OnBehalfOf changed the hash")
	}
}

func TestHash_OnBehalfOf_ChangesHash(t *testing.T) {
	tx1, _ := NewTransaction("chain", TxTransfer, "aabb", 0, 0, TransferPayload{To: "ccdd", Amount: 100})
	tx2, _ := NewTransaction("chain", TxTransfer, "aabb", 0, 0, TransferPayload{To: "ccdd", Amount: 100})
	tx2.OnBehalfOf = "eeff1122"
	if tx1.Hash() == tx2.Hash() {
		t.Error("OnBehalfOf should change the hash")
	}
}
```

- [ ] **Step 3: 전체 core 테스트 실행**

Run: `cd tolchain && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe test ./core/... -v -count=1`
Expected: 모든 테스트 통과

- [ ] **Step 4: 커밋**

```bash
cd tolchain && git add core/transaction.go core/transaction_test.go && git commit -m "feat: add OnBehalfOf field to Transaction for delegation"
```

---

### Task 7: Delegation 타입 정의 + 이벤트

**Files:**
- Modify: `tolchain/core/transaction.go` — TxType 상수, Payload 구조체, DelegationGrant
- Modify: `tolchain/events/emitter.go` — 이벤트 타입

- [ ] **Step 1: TxType 상수 2개 추가**

`core/transaction.go` TxType 상수 블록 끝에:

```go
	TxGrantDelegation  TxType = "grant_delegation"
	TxRevokeDelegation TxType = "revoke_delegation"
```

- [ ] **Step 2: Payload + DelegationGrant 구조체 추가**

Payload 정의 영역 끝에:

```go
type GrantDelegationPayload struct {
	Grantee    string   `json:"grantee"`
	AllowTypes []string `json:"allow_types"`
	ExpiresAt  int64    `json:"expires_at"`
	MaxUses    uint64   `json:"max_uses"`
	MaxAmount  uint64   `json:"max_amount"` // TODO: SpentAmount tracking (requires Handler interface change)
}

type RevokeDelegationPayload struct {
	Grantee string `json:"grantee"`
}

type DelegationGrant struct {
	Granter     string   `json:"granter"`
	Grantee     string   `json:"grantee"`
	AllowTypes  []string `json:"allow_types"`
	ExpiresAt   int64    `json:"expires_at"`
	MaxUses     uint64   `json:"max_uses"`
	UsedCount   uint64   `json:"used_count"`
	MaxAmount   uint64   `json:"max_amount"`
	SpentAmount uint64   `json:"spent_amount"`
	CreatedAt   int64    `json:"created_at"`
}
```

- [ ] **Step 3: 이벤트 타입 추가**

`events/emitter.go` 이벤트 상수 블록 끝에:

```go
	EventDelegationGranted EventType = "delegation_granted"
	EventDelegationRevoked EventType = "delegation_revoked"
```

- [ ] **Step 4: 빌드 확인**

Run: `cd tolchain && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe build ./...`

- [ ] **Step 5: 커밋**

```bash
cd tolchain && git add core/transaction.go events/emitter.go && git commit -m "feat: delegation types, payloads, and event definitions"
```

---

### Task 8: State 인터페이스에 Delegation 메서드 추가

**Files:**
- Modify: `tolchain/core/state.go` — State 인터페이스에 3개 메서드 추가
- Modify: `tolchain/storage/statedb.go` — 구현
- Modify: `tolchain/internal/testutil/` — 테스트 mock 구현

> **구현 에이전트에게**: 먼저 `core/state.go`를 읽어서 State 인터페이스의 현재 메서드 목록을 확인할 것. 기존 패턴(GetAccount/SetAccount, GetAsset/SetAsset 등)을 따라 아래 메서드를 추가.

- [ ] **Step 1: State 인터페이스에 Delegation 메서드 추가**

`core/state.go`의 `State` 인터페이스에:

```go
	GetDelegation(granter, grantee string) (*DelegationGrant, error)
	SetDelegation(grant *DelegationGrant) error
	DeleteDelegation(granter, grantee string) error
```

- [ ] **Step 2: StateDB 구현**

`storage/statedb.go`에 구현. 키 패턴: `deleg:{granter}:{grantee}`. 기존 `GetAsset`/`SetAsset` 패턴을 그대로 따름 (JSON marshal/unmarshal + `s.get(key)` / `s.put(key, data)`).

```go
func (s *StateDB) GetDelegation(granter, grantee string) (*core.DelegationGrant, error) {
	key := "deleg:" + granter + ":" + grantee
	data, err := s.get(key)
	if err != nil {
		return nil, err
	}
	var grant core.DelegationGrant
	if err := json.Unmarshal(data, &grant); err != nil {
		return nil, err
	}
	return &grant, nil
}

func (s *StateDB) SetDelegation(grant *core.DelegationGrant) error {
	key := "deleg:" + grant.Granter + ":" + grant.Grantee
	data, err := json.Marshal(grant)
	if err != nil {
		return err
	}
	return s.put(key, data)
}

func (s *StateDB) DeleteDelegation(granter, grantee string) error {
	key := "deleg:" + granter + ":" + grantee
	return s.delete(key)
}
```

- [ ] **Step 3: 테스트 mock 업데이트**

`internal/testutil/`의 mock State 구현체에도 동일한 3개 메서드 추가. 실제 구현은 인메모리 map 사용.

- [ ] **Step 4: 빌드 + 전체 테스트 실행**

Run: `cd tolchain && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe test ./... -count=1`
Expected: 기존 테스트 전부 통과

- [ ] **Step 5: 커밋**

```bash
cd tolchain && git add core/state.go storage/statedb.go internal/testutil/ && git commit -m "feat: delegation state methods (Get/Set/Delete)"
```

---

### Task 9: Delegation VM 모듈 — grant/revoke 핸들러

**Files:**
- Create: `tolchain/vm/modules/delegation/delegation.go`
- Modify: 블랭크 임포트 파일 (cmd/node/ 또는 imports.go)

> **구현 에이전트에게**: `time.Now().Unix()` 대신 `ctx.Block.Header.Timestamp`을 사용할 것 (합의 결정론성).

- [ ] **Step 1: delegation 모듈 작성**

```go
package delegation

import (
	"encoding/json"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func init() {
	vm.Register(core.TxGrantDelegation, handleGrantDelegation)
	vm.Register(core.TxRevokeDelegation, handleRevokeDelegation)
}

func handleGrantDelegation(ctx *vm.Context, payload json.RawMessage) error {
	var p core.GrantDelegationPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode grant_delegation payload: %w", err)
	}
	if err := vm.ValidatePubKey(p.Grantee, "grantee"); err != nil {
		return err
	}
	if p.Grantee == ctx.Tx.From {
		return fmt.Errorf("cannot delegate to self")
	}
	if len(p.AllowTypes) == 0 {
		return fmt.Errorf("allow_types must not be empty")
	}
	for _, t := range p.AllowTypes {
		if core.TxType(t) == core.TxGrantDelegation || core.TxType(t) == core.TxRevokeDelegation {
			return fmt.Errorf("cannot delegate %s", t)
		}
	}

	grant := core.DelegationGrant{
		Granter:    ctx.Tx.From,
		Grantee:    p.Grantee,
		AllowTypes: p.AllowTypes,
		ExpiresAt:  p.ExpiresAt,
		MaxUses:    p.MaxUses,
		MaxAmount:  p.MaxAmount,
		CreatedAt:  ctx.Block.Header.Timestamp,
	}
	if err := ctx.State.SetDelegation(&grant); err != nil {
		return fmt.Errorf("store delegation grant: %w", err)
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventDelegationGranted,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data: map[string]any{
				"granter": ctx.Tx.From, "grantee": p.Grantee,
				"allow_types": p.AllowTypes, "expires_at": p.ExpiresAt,
			},
		})
	}
	return nil
}

func handleRevokeDelegation(ctx *vm.Context, payload json.RawMessage) error {
	var p core.RevokeDelegationPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode revoke_delegation payload: %w", err)
	}
	if err := vm.ValidatePubKey(p.Grantee, "grantee"); err != nil {
		return err
	}
	if _, err := ctx.State.GetDelegation(ctx.Tx.From, p.Grantee); err != nil {
		return fmt.Errorf("delegation grant not found: %w", err)
	}
	if err := ctx.State.DeleteDelegation(ctx.Tx.From, p.Grantee); err != nil {
		return fmt.Errorf("delete delegation grant: %w", err)
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventDelegationRevoked,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"granter": ctx.Tx.From, "grantee": p.Grantee},
		})
	}
	return nil
}
```

- [ ] **Step 2: 블랭크 임포트 등록**

기존 모듈 임포트를 확인: `grep -rn 'modules/' cmd/node/`
해당 파일에 추가: `_ "github.com/tolelom/tolchain/vm/modules/delegation"`

- [ ] **Step 3: 빌드 확인**

Run: `cd tolchain && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe build ./...`

- [ ] **Step 4: 커밋**

```bash
cd tolchain && git add vm/modules/delegation/ cmd/node/ && git commit -m "feat: delegation VM module with grant/revoke handlers"
```

---

### Task 10: Executor에 Delegation 검증 + EffectiveSender

**Files:**
- Modify: `tolchain/vm/executor.go`
- Modify: `tolchain/vm/helpers.go` — RequireOperator 수정
- Modify: `tolchain/vm/modules/economy/token.go`
- Modify: `tolchain/vm/modules/asset/asset.go`
- Modify: `tolchain/vm/modules/session/session.go`
- Modify: `tolchain/vm/modules/market/market.go`
- Modify: `tolchain/vm/modules/inventory/inventory.go`
- Modify: `tolchain/vm/modules/random/random.go`
- Modify: `tolchain/vm/modules/reward/reward.go`

- [ ] **Step 1: Context에 EffectiveSender 필드 추가**

`vm/executor.go`의 `Context` (L14-21):

```go
type Context struct {
	State           core.State
	Block           *core.Block
	Tx              *core.Transaction
	Emitter         events.EventEmitter
	Operators       map[string]bool
	MaxTotalSupply  uint64
	EffectiveSender string // tx.From if direct, tx.OnBehalfOf if delegated
}
```

- [ ] **Step 2: validateDelegation 헬퍼 작성**

`vm/executor.go`에 추가 (import `"time"` 필요):

```go
func (e *Executor) validateDelegation(tx *core.Transaction) error {
	if tx.Type == core.TxGrantDelegation || tx.Type == core.TxRevokeDelegation {
		return fmt.Errorf("cannot execute %s via delegation", tx.Type)
	}
	grant, err := e.state.GetDelegation(tx.OnBehalfOf, tx.From)
	if err != nil {
		return fmt.Errorf("grant not found: %w", err)
	}
	if grant.ExpiresAt > 0 && time.Now().Unix() > grant.ExpiresAt {
		_ = e.state.DeleteDelegation(tx.OnBehalfOf, tx.From)
		return fmt.Errorf("delegation expired")
	}
	if grant.MaxUses > 0 && grant.UsedCount >= grant.MaxUses {
		return fmt.Errorf("delegation max uses exceeded")
	}
	allowed := false
	for _, t := range grant.AllowTypes {
		if core.TxType(t) == tx.Type {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("tx type %s not allowed by delegation", tx.Type)
	}
	grant.UsedCount++
	if err := e.state.SetDelegation(grant); err != nil {
		return fmt.Errorf("update delegation grant: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: applyTx 수정 — effectiveSender 도입**

`applyTx` (L127-187)에서:
1. 함수 시작 부분에 effectiveSender 결정 로직 추가
2. `tx.From` 참조를 `effectiveSender`로 교체 (계정 로드, nonce 검증, fee 처리, proposer 비교 모두)
3. Context 생성 시 `EffectiveSender: effectiveSender` 설정

```go
func (e *Executor) applyTx(block *core.Block, tx *core.Transaction) error {
	effectiveSender := tx.From
	if tx.OnBehalfOf != "" {
		if err := e.validateDelegation(tx); err != nil {
			return fmt.Errorf("delegation: %w", err)
		}
		effectiveSender = tx.OnBehalfOf
	}

	acc, err := e.state.GetAccount(effectiveSender)
	// ... 이하 tx.From → effectiveSender 교체:
	// L132: acc.Nonce != tx.Nonce → 동일 (nonce는 effectiveSender 기준)
	// L135: acc.Balance < tx.Fee → 동일
	// L150: tx.From != block.Header.Proposer → effectiveSender != block.Header.Proposer
	// L170: tx.From == block.Header.Proposer → effectiveSender == block.Header.Proposer
	// Context 생성 시 EffectiveSender 설정
```

- [ ] **Step 4: RequireOperator를 EffectiveSender로 수정**

`vm/helpers.go`의 `RequireOperator`:

```go
func RequireOperator(ctx *Context) error {
	if len(ctx.Operators) == 0 {
		return nil
	}
	if !ctx.Operators[ctx.EffectiveSender] {
		return fmt.Errorf("not an operator: %s: %w", ctx.EffectiveSender, core.ErrUnauthorized)
	}
	return nil
}
```

이로써 오퍼레이터 전용 TX(mint_asset, grant_reward 등)는 delegation으로 실행해도 **granter(위임자)가 오퍼레이터여야** 통과.

- [ ] **Step 5: 기존 VM 모듈에서 ctx.Tx.From → ctx.EffectiveSender 교체**

각 모듈에서 sender 계정을 참조하는 `ctx.Tx.From`을 `ctx.EffectiveSender`로 교체:

| 파일 | 변경 대상 |
|------|----------|
| `economy/token.go` L31 | `ctx.State.GetAccount(ctx.Tx.From)` → `ctx.State.GetAccount(ctx.EffectiveSender)` |
| `asset/asset.go` | `ctx.Tx.From` owner 참조 → `ctx.EffectiveSender` |
| `market/market.go` | 리스팅 소유자 확인, 구매자 계정 로드 → `ctx.EffectiveSender` |
| `session/session.go` | 세션 오픈 시 From → `ctx.EffectiveSender` |
| `inventory/inventory.go` | 장비 소유자 확인 → `ctx.EffectiveSender` |
| `random/random.go` | commit/reveal From → `ctx.EffectiveSender` |
| `reward/reward.go` | RequireOperator는 이미 수정됨, 나머지 From 참조 확인 |

> **구현 에이전트에게**: `grep -rn 'ctx.Tx.From' vm/modules/`로 전체 참조를 확인하고, 각각 `ctx.EffectiveSender`로 교체. 이벤트 Data의 `"from"` 값도 `ctx.EffectiveSender`로.

- [ ] **Step 6: 빌드 + 기존 테스트 실행**

Run: `cd tolchain && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe test ./... -count=1`
Expected: 기존 테스트 전부 통과 (EffectiveSender는 non-delegation TX에서 tx.From과 동일)

- [ ] **Step 7: 커밋**

```bash
cd tolchain && git add vm/executor.go vm/helpers.go vm/modules/ && git commit -m "feat: delegation validation in Executor + EffectiveSender across all modules"
```

---

### Task 11: wallet 패키지 — Delegation TX 빌더

**Files:**
- Modify: `tolchain/wallet/wallet.go`
- Modify: `tolchain/wallet/wallet_test.go`

- [ ] **Step 1: Delegation TX 빌더 추가**

`wallet/wallet.go` 끝에:

```go
func (w *Wallet) GrantDelegation(chainID, grantee string, allowTypes []string, expiresAt int64, maxUses, maxAmount, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxGrantDelegation, nonce, fee, core.GrantDelegationPayload{
		Grantee: grantee, AllowTypes: allowTypes, ExpiresAt: expiresAt, MaxUses: maxUses, MaxAmount: maxAmount,
	})
}

func (w *Wallet) RevokeDelegation(chainID, grantee string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxRevokeDelegation, nonce, fee, core.RevokeDelegationPayload{Grantee: grantee})
}

func (w *Wallet) NewDelegatedTx(chainID string, typ core.TxType, onBehalfOf string, nonce, fee uint64, payload any) (*core.Transaction, error) {
	tx, err := core.NewTransaction(chainID, typ, w.pub.Hex(), nonce, fee, payload)
	if err != nil {
		return nil, err
	}
	tx.OnBehalfOf = onBehalfOf
	tx.Sign(w.priv)
	return tx, nil
}
```

- [ ] **Step 2: 테스트 추가**

`wallet/wallet_test.go`에 `TestGrantDelegation`, `TestRevokeDelegation`, `TestNewDelegatedTx` 추가 (TX 생성 + 서명 검증 + OnBehalfOf 확인).

- [ ] **Step 3: 테스트 실행**

Run: `cd tolchain && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe test ./wallet/... -v -count=1`

- [ ] **Step 4: 커밋**

```bash
cd tolchain && git add wallet/ && git commit -m "feat: delegation TX builders in wallet package"
```

---

### Task 12: RPC API 확장 — Delegation 조회

**Files:**
- Modify: `tolchain/rpc/handler.go`
- Modify: `tolchain/indexer/indexer.go` (getDelegationsByGrantee용)

- [ ] **Step 1: Dispatch에 delegation 메서드 추가**

`rpc/handler.go`의 `Dispatch` switch에:

```go
case "getDelegation":
	return h.getDelegation(req)
case "getDelegationsByGranter":
	return h.getDelegationsByGranter(req)
case "getDelegationsByGrantee":
	return h.getDelegationsByGrantee(req)
```

- [ ] **Step 2: getDelegation 핸들러 구현**

```go
func (h *Handler) getDelegation(req Request) Response {
	var params struct {
		Granter string `json:"granter"`
		Grantee string `json:"grantee"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "invalid params")
	}
	h.state.BlockRLock()
	grant, err := h.state.GetDelegation(params.Granter, params.Grantee)
	h.state.BlockRUnlock()
	if err != nil {
		return errorResponse(req.ID, CodeInternalError, "delegation not found")
	}
	return successResponse(req.ID, grant)
}
```

- [ ] **Step 3: getDelegationsByGranter / getDelegationsByGrantee 구현**

> **구현 에이전트에게**: 이 두 메서드는 prefix scan이 필요함. 기존 `getAssetsByOwner`가 indexer를 사용하는 패턴 참고. State 인터페이스에 `GetDelegationsByGranter(granter string) ([]DelegationGrant, error)` 추가하거나, indexer에 delegation 이벤트 구독을 추가하여 보조 인덱스(`deleg_granter:{pubkey}` → `[grantee1, grantee2, ...]`) 유지.

- [ ] **Step 4: 빌드 확인**

Run: `cd tolchain && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe build ./...`

- [ ] **Step 5: 커밋**

```bash
cd tolchain && git add rpc/handler.go indexer/ core/state.go storage/ && git commit -m "feat: delegation query RPC methods"
```

---

### Task 13: 통합 테스트

**Files:**
- Create: `tolchain/tests/delegation_test.go`

- [ ] **Step 1: E2E 통합 테스트 작성**

기존 통합 테스트 패턴(`tolchain/tests/`)을 참고:

```go
func TestDelegation_E2E(t *testing.T) {
	// 1. granter에게 토큰 배분
	// 2. grant_delegation TX (grantee에게 transfer 권한 위임)
	// 3. grantee가 OnBehalfOf=granter로 transfer TX
	// 4. granter 잔액 감소, grantee 잔액 변동 없음
	// 5. revoke_delegation TX
	// 6. grantee 재시도 → 실패
}

func TestDelegation_E2E_MaxUses(t *testing.T) {
	// max_uses=2 → 2회 성공, 3회째 실패
}

func TestDelegation_E2E_Expiry(t *testing.T) {
	// expires_at 과거 → 즉시 실패
}

func TestDelegation_E2E_TypeNotAllowed(t *testing.T) {
	// allow_types에 없는 TxType → 실패
}

func TestDelegation_E2E_PreventReDelegation(t *testing.T) {
	// OnBehalfOf로 grant_delegation → 거부
}
```

- [ ] **Step 2: 테스트 실행**

Run: `cd tolchain && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe test ./tests/... -run TestDelegation -v -count=1`

- [ ] **Step 3: 전체 테스트 실행**

Run: `cd tolchain && C:/Users/SSAFY/sdk/go1.25.5/bin/go.exe test ./... -count=1`
Expected: 전체 통과

- [ ] **Step 4: 커밋**

```bash
cd tolchain && git add tests/delegation_test.go && git commit -m "test: delegation E2E integration tests"
```
