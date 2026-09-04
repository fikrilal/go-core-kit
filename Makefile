SHELL := /bin/bash

GO ?= go

.PHONY: verify fmt fmt-check vet lint test build tidy

verify: fmt-check vet lint test build

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt required for:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

lint:
	golangci-lint run

test:
	$(GO) test ./...

build:
	$(GO) build ./...

tidy:
	$(GO) mod tidy
