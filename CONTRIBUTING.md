# 기여

Go 1.26 이상이 필요합니다.

```bash
go build ./...            # 빌드
go test ./...             # 테스트
go vet ./...              # 정적 검사
```

Makefile은 같은 명령에 크로스 컴파일을 더합니다.

```bash
make build                # 현재 OS/아키텍처, 결과물 tolchain-node
make darwin-arm64         # macOS Apple Silicon
make linux-amd64          # Linux x86_64
make test                 # go test ./...
make vet                  # go vet ./...
```

패키지 구성은 `ARCHITECTURE.md`에 있습니다.

## 코드 스타일

- Go 표준 관례(`gofmt`, `go vet`)를 따릅니다.
- 패키지 주석과 공개 함수 주석은 영어로 씁니다.
- 외부 의존성은 goleveldb, x/crypto, prometheus/client_golang으로 최소한만 둡니다.

## 테스트

- 패키지 단위 테스트는 각 패키지 옆 `_test.go`에, 통합 테스트와 벤치마크는 `tests/`에 있습니다.
- 저장소는 디스크 대신 `internal/testutil.NewMemDB()`를 씁니다.
- VM 모듈을 거치는 테스트는 해당 모듈을 blank import합니다.

```go
import (
    _ "github.com/tolelom/tolchain/vm/modules/asset"
    _ "github.com/tolelom/tolchain/vm/modules/economy"
)
```

- CI는 전체 커버리지 50% 이상을 요구합니다. CI와 같은 범위로 로컬에서 확인합니다.

```bash
go test -race ./... -coverprofile=coverage.out -coverpkg=./...
go tool cover -func=coverage.out | tail -1
```

- 벤치마크는 `go test ./tests/ -bench=. -benchmem -run=^$`로 돌립니다.

## 커밋 메시지

접두어와 짧은 설명으로 씁니다.

```
feat: add inventory slot validation
fix: prevent double-reveal in random module
chore: update CI coverage threshold
docs: add RPC examples to README
test: add marketplace edge case tests
```

## Pull Request

1. `main`에서 기능 브랜치를 만듭니다.
2. `go test ./...`와 `go vet ./...`가 통과하는지 확인합니다.
3. PR 하나에는 기능이나 수정 하나만 담습니다.

CI는 gofmt, `go mod tidy -diff`, vet, 커버리지 게이트를 포함한 테스트를 돌리고, `main`에 push되면 GHCR 이미지를 빌드해 올립니다.

## 새 트랜잭션 종류 추가

tx 타입, 핸들러 등록, 이벤트 타입과 payload는 `design/tolchain.yaml`이 기준이고, `*.gen.go`는 daemok이 만듭니다. 생성 파일은 직접 고치지 않습니다.

1. `design/tolchain.yaml`: tx 타입, 핸들러 등록, 이벤트 선언
2. `daemok gen`: `core/tx_type.gen.go`, `events/*.gen.go`, `vm/modules/<name>/register.gen.go`, `cmd/node/modules.gen.go` 다시 생성
3. `core/transaction.go`: payload 구조체
4. `core/state.go`: 도메인 타입과 `State` 인터페이스 메서드(필요할 때)
5. `storage/statedb.go`: 키 접두사 등록, Get/Set 구현
6. `vm/modules/<name>/`: 핸들러 본문. 시그니처가 다르면 `register.gen.go`에서 컴파일이 깨집니다
7. `rpc/handler.go`: 조회 RPC 메서드
8. `wallet/wallet.go`: tx 빌더 헬퍼
9. 테스트(VM 모듈 blank import 포함)
10. `daemok check`: 설계 검사, import 대조, 생성 파일 최신 여부 확인

## 이슈 제보

GitHub 이슈에 다음을 적습니다.

- 재현 절차
- 기대한 동작과 실제 동작
- Go 버전과 OS
