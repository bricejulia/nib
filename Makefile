BINARY   := kiwi
GO       := go
GOLANGCI := golangci-lint
GOVULNCHECK := govulncheck

.PHONY: all build run dev generate lint vet tidy test clean

all: build

## build: compile the binary
build:
	$(GO) build -o $(BINARY) cmd/kiwi/main.go

## installl: install the binary
install:
	$(GO) install ./cmd/kiwi

## run: run the server (loads env from .env)
run:
	$(GO) run cmd/kiwi/main.go

## vet: run go vet
vet:
	$(GO) vet ./...

## tidy: tidy and verify go.mod / go.sum
tidy:
	$(GO) mod tidy
	$(GO) mod verify

## test: run all tests
test:
	$(GO) test -race ./...

## lint: run golangci-lint (depends on vet)
lint: vet
	$(GOLANGCI) run ./...

## audit: run govulncheck
audit:
	$(GOVULNCHECK) ./...

## clean: remove build artifacts
clean:
	rm -f $(BINARY)

## lint-install: install golangci-lint to GOPATH/bin
lint-install:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$($(GO) env GOPATH)/bin v1.64.8