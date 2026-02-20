# Open Issues

## 1. Migrate logging to `log/slog` (structured logging)

**Priority:** Medium
**Scope:** All packages

현재 모든 로깅이 `log.Printf`를 사용 중. 프로덕션에서는 레벨 구분(debug/info/warn/error), JSON 출력, 로그 수집(ELK/Loki) 연동이 필요.

### 작업 내용
- `log.Printf` → `slog.Info`/`slog.Warn`/`slog.Error` 전환
- `log.Fatalf` → `slog.Error` + `os.Exit(1)` 또는 유지
- `cmd/node/main.go`에서 로그 레벨/포맷 설정 (JSON vs text)
- config에 `log_level` 필드 추가

### 영향 파일
consensus/poa.go, network/node.go, network/sync.go, indexer/indexer.go, rpc/server.go, cmd/node/main.go 등 전체

---

## 2. Add test coverage for core packages

**Priority:** Medium
**Scope:** consensus, storage, config, wallet

현재 테스트가 `tests/` 패키지에만 존재. 핵심 패키지별 유닛 테스트가 전무.

### 필요한 테스트
- **consensus**: `IsProposer()` 라운드로빈, `ProduceBlock()` 정상/실패, `ValidateBlock()` 정상/서명불일치/높이불일치
- **storage**: `StateDB.Snapshot()`/`RevertToSnapshot()`/`Commit()` 라운드트립, `ComputeRoot()` 결정론적 해시, `LevelBlockStore.CommitBlock()` 원자성
- **config**: `Load()` + `Validate()` — 유효/무효 케이스, `DefaultConfig()` 동작, `Save()` 권한 확인
- **wallet**: `SaveKey`/`LoadKey` 라운드트립, 잘못된 비밀번호 거부, 빈 비밀번호 동작

### 참고
- `internal/testutil`의 `MemDB`/`MemBlockStore` 활용
- 각 패키지 내부에 `_test.go` 생성 (package-level 테스트)
