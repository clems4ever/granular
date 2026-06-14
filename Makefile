# Makefile for granular

BIN_DIR := bin
GRANULAR := $(BIN_DIR)/granular
GRANULAR_SERVER := $(BIN_DIR)/granular-server

GO ?= go

.DEFAULT_GOAL := build

.PHONY: all build run-server fmt vet test test-race check codespec tidy clean install help

## all: tidy, check and build
all: tidy check build

## build: compile both binaries into ./bin
build: $(GRANULAR) $(GRANULAR_SERVER)

$(GRANULAR): $(shell find . -name '*.go' -not -name '*_test.go')
	$(GO) build -o $(GRANULAR) ./cmd/granular

$(GRANULAR_SERVER): $(shell find . -name '*.go' -not -name '*_test.go')
	$(GO) build -o $(GRANULAR_SERVER) ./cmd/granular-server

## run-server: build and run granular-server (override vars, e.g. GRANULAR_GITHUB_TOKEN=...)
run-server: $(GRANULAR_SERVER)
	./$(GRANULAR_SERVER)

## fmt: format all Go sources
fmt:
	$(GO) fmt ./...

## vet: run go vet
vet:
	$(GO) vet ./...

## test: run the test suite
test:
	$(GO) test ./...

## test-race: run the test suite with the race detector
test-race:
	$(GO) test -race ./...

## codespec: verify function docs are in sync (same check the Stop hook runs)
codespec:
	codespec check --all --exit

## check: fmt, vet, codespec and test
check: fmt vet codespec test

## tidy: sync go.mod / go.sum
tidy:
	$(GO) mod tidy

## install: install both binaries into GOBIN/GOPATH
install:
	$(GO) install ./cmd/granular ./cmd/granular-server

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR)

## help: list available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
