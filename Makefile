.DEFAULT_GOAL := all

.PHONY: all build test cover lint fmt install clean

all: fmt lint test build

build:
	go build -o squawk ./cmd/squawk

test:
	go test -race ./...

cover:
	go test -cover ./...

lint:
	go vet ./...
	@which staticcheck > /dev/null 2>&1 && staticcheck ./... || true

fmt:
	gofmt -w .
	@which goimports > /dev/null 2>&1 && goimports -w . || true

install:
	go install ./cmd/squawk

clean:
	rm -f squawk
