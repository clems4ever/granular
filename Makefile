# Makefile for granular

BIN_DIR := bin

# The four binaries that make up granular:
#   granular-client          agent CLI (builds proposals, runs operations)
#   granular-auth-server     authorization server (policy authority + consent UI)
#   granular-github-resource-server  GitHub resource server (holds the credential, executes ops)
#   granular-subject          admin CLI for subject-token lifecycle
CMDS := granular-client granular-auth-server granular-github-resource-server granular-subject
BINS := $(addprefix $(BIN_DIR)/,$(CMDS))

GO ?= go

.DEFAULT_GOAL := build

.PHONY: all build run-auth-server run-resource-server fmt vet test test-race check codespec tidy clean install help

## all: tidy, check and build
all: tidy check build

## build: compile all binaries into ./bin
build: $(BINS)

$(BIN_DIR)/%: $(shell find . -name '*.go' -not -name '*_test.go')
	$(GO) build -o $@ ./cmd/$*

## run-auth-server: build and run the authorization server (loads ./granular-auth.yaml)
run-auth-server: $(BIN_DIR)/granular-auth-server
	./$(BIN_DIR)/granular-auth-server

## run-resource-server: build and run the GitHub resource server (loads ./granular-github-resource-server.yaml)
run-resource-server: $(BIN_DIR)/granular-github-resource-server
	./$(BIN_DIR)/granular-github-resource-server

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

## install: install all binaries into GOBIN/GOPATH
install:
	$(GO) install $(addprefix ./cmd/,$(CMDS))

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR)

## help: list available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
