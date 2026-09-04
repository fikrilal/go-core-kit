SHELL := /bin/bash

GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= v2.14.0

.PHONY: verify fmt fmt-check vet lint test build tidy tools

verify: fmt-check vet lint test build

tools:
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b "$$HOME/.local/bin" $(GOLANGCI_LINT_VERSION)
	@$(GOLANGCI_LINT) version | head -n 1

fmt:
	$(GOLANGCI_LINT) fmt

fmt-check:
	$(GOLANGCI_LINT) fmt --diff

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run

test:
	$(GO) test ./...

build:
	$(GO) build ./...

tidy:
	$(GO) mod tidy
