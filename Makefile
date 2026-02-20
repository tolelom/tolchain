APP      := tolchain-node
CMD      := ./cmd/node
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)

.PHONY: build test vet clean darwin-arm64

## build: compile for current OS/arch
build:
	go build -ldflags "$(LDFLAGS)" -o $(APP) $(CMD)

## test: run all tests
test:
	go test ./...

## vet: static analysis
vet:
	go vet ./...

## darwin-arm64: cross-compile for Mac M-series
darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(APP)-darwin-arm64 $(CMD)

## linux-amd64: cross-compile for Linux x86_64
linux-amd64:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(APP)-linux-amd64 $(CMD)

## clean: remove build artifacts
clean:
	rm -f $(APP) $(APP)-darwin-arm64 $(APP)-linux-amd64
