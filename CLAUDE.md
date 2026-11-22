# CLAUDE.md

Claude Code가 이 저장소에서 작업할 때 보는 지침입니다. 구조와 지켜야 할 규칙은 `ARCHITECTURE.md`, 빌드·테스트·새 트랜잭션 종류 추가 절차는 `CONTRIBUTING.md`, 설정·RPC·트랜잭션·이벤트·메트릭 값은 `docs/reference.md`에 있습니다.

## 검증

- 끝내기 전에 `gofmt -l .`이 비어 있는지, `go vet ./...`와 `go test ./...`가 통과하는지 확인합니다. CI는 여기에 `go mod tidy -diff`와 커버리지 50% 게이트를 더합니다.
- `design/tolchain.yaml`을 고쳤으면 `daemok gen`으로 생성 파일을 다시 만들고 `daemok check`를 돌립니다.

## 작업 습관

- tx 타입, 이벤트, RPC 메서드, 설정 칸을 더하거나 바꾸면 `docs/reference.md`의 해당 표도 함께 고칩니다.
- golden 파일 테스트가 깨지면 `-update`로 덮기 전에 바이트가 바뀐 이유부터 찾습니다. 의도한 형식 변경이 아니면 코드를 고칩니다.
