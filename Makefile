VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.DEFAULT_GOAL := all

.PHONY: all build test cover lint fmt install clean

all: fmt lint test build

build:
	go build -ldflags "-X main.version=$(VERSION)" -o squawk ./cmd/squawk

test:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1

lint:
	go vet ./...
	@which staticcheck > /dev/null 2>&1 && staticcheck ./... || true

fmt:
	gofmt -w .
	@which goimports > /dev/null 2>&1 && goimports -w . || true

install:
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/squawk

clean:
	rm -f squawk coverage.out
