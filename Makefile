# Makefile for baryo-cli

BINARY   := baryo
MODULE   := github.com/arnelirobles/baryo-cli
VERSION  ?= dev

LDFLAGS  := -s -w -X $(MODULE)/internal/cli.Version=$(VERSION)

.PHONY: all build build-nocgo test test-nocgo vet lint fmt clean

all: vet test build

## Build targets ───────────────────────────────────────────────

build:
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

build-nocgo:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## Quality checks ──────────────────────────────────────────────

test:
	CGO_ENABLED=1 go test -race -count=1 ./...

test-nocgo:
	CGO_ENABLED=0 go test -count=1 ./...

test-cover:
	CGO_ENABLED=1 go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

vet:
	go vet ./...

fmt:
	gofmt -s -w .

lint:
	golangci-lint run ./...

## Cleanup ─────────────────────────────────────────────────────

clean:
	rm -f $(BINARY) coverage.out
