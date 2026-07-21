# Makefile for cairn

BINARY      := cairn
BUILD_DIR   := .
INSTALL_DIR := $(HOME)/.local/bin

GIT_TAG := $(shell git describe --tags --exact-match 2>/dev/null | sed 's/^v//')
VERSION := $(if $(GIT_TAG),$(GIT_TAG),dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -X github.com/quad341/cairn/cmd.version=$(VERSION) \
           -X github.com/quad341/cairn/cmd.commit=$(COMMIT) \
           -X github.com/quad341/cairn/cmd.date=$(DATE)

.PHONY: all build test install fmt fmt-check clean help

all: build

## build: compile the cairn binary with version metadata
build:
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) .

## test: run all tests (race-enabled, matching CI)
test:
	go test ./... -race -count=1

## install: build and install cairn to ~/.local/bin
install: build
	@mkdir -p $(INSTALL_DIR)
	@set -e; \
	tmp="$(INSTALL_DIR)/.$(BINARY).tmp.$$$$"; \
	trap 'rm -f "$$tmp"' EXIT INT TERM HUP; \
	cp -f "$(BUILD_DIR)/$(BINARY)" "$$tmp"; \
	chmod 0755 "$$tmp"; \
	mv -f "$$tmp" "$(INSTALL_DIR)/$(BINARY)"; \
	trap - EXIT INT TERM HUP
	@echo "Installed $(BINARY) to $(INSTALL_DIR)/$(BINARY)"

## fmt: format all Go files
fmt:
	golangci-lint fmt ./...

## fmt-check: check Go formatting (for CI)
fmt-check:
	golangci-lint fmt -d ./...

## clean: remove build artifacts
clean:
	rm -f $(BUILD_DIR)/$(BINARY)

## help: show this help message
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
