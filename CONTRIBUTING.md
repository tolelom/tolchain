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
- Keep external dependencies minimal (currently 2: goleveldb, x/crypto)

## Testing

- All tests live in `tests/` (integration test package)
- Use `internal/testutil.NewMemDB()` for in-memory storage — no disk I/O
- VM module tests require blank imports for the modules under test:

```go
import (
    _ "github.com/tolelom/tolchain/vm/modules/asset"
    _ "github.com/tolelom/tolchain/vm/modules/economy"
    // ... other modules as needed
)
```

- CI requires **50% minimum coverage**
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
4. Keep PRs focused — one feature or fix per PR

## Adding a New Transaction Type

Follow the checklist in [README.md](README.md#adding-a-new-transaction-type).

## Reporting Issues

Open an issue on GitHub with:
- Steps to reproduce
- Expected vs actual behavior
- Go version and OS
