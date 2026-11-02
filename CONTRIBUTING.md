# Contributing to TOL Chain

## Development Setup

```bash
# Clone
git clone https://github.com/tolelom/tolchain.git
cd tolchain

# Build
go build ./...

# Test
go test ./tests/ -v

# Static analysis
go vet ./...
```

Requires **Go 1.24+**.

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Package-level and exported function comments in English
- Keep external dependencies minimal (currently goleveldb, x/crypto, prometheus/client_golang)

## Testing

- All tests live in `tests/` (integration test package)
- Use `internal/testutil.NewMemDB()` for in-memory storage - no disk I/O
- VM module tests require blank imports for the modules under test:

```go
import (
    _ "github.com/tolelom/tolchain/vm/modules/asset"
    _ "github.com/tolelom/tolchain/vm/modules/economy"
    // ... other modules as needed
)
```

- CI requires **50% minimum coverage**. Check it locally:

```bash
go test ./tests/ -coverprofile=coverage.out -coverpkg=./...
go tool cover -func=coverage.out | tail -1
```

- Run benchmarks with `go test ./tests/ -bench=. -benchmem -run=^$`

## Commit Messages

Use short, descriptive messages in the form:

```
feat: add inventory slot validation
fix: prevent double-reveal in random module
chore: update CI coverage threshold
docs: add RPC examples to README
test: add marketplace edge case tests
```

## Pull Requests

1. Create a feature branch from `main`
2. Ensure all tests pass: `go test ./tests/`
3. Ensure static analysis passes: `go vet ./...`
4. Keep PRs focused - one feature or fix per PR

## Adding a New Transaction Type

1. `core/transaction.go` - add `TxType` constant + payload struct
2. `core/state.go` - add domain type + `State` interface methods (if needed)
3. `storage/statedb.go` - register prefix + implement Get/Set
4. `events/emitter.go` - add `EventType` constant
5. `vm/modules/<name>/` - implement handler + `init()` registration
6. `rpc/handler.go` - add query RPC method
7. `wallet/wallet.go` - add tx builder helper
8. `cmd/node/main.go` - add blank import
9. `tests/` - write tests (with blank imports for VM modules)

## Reporting Issues

Open an issue on GitHub with:
- Steps to reproduce
- Expected vs actual behavior
- Go version and OS
