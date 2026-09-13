SHELL := /bin/bash

GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= v2.14.0
SQLC_VERSION ?= v1.31.1

.PHONY: verify fmt fmt-check vet lint test build build-pg tidy tools generate drift

verify:
	$(GO) run ./tools/gokit verify --profile full

tools:
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b "$$HOME/.local/bin" $(GOLANGCI_LINT_VERSION)
	@command -v sqlc >/dev/null 2>&1 || { \
		os="$$(uname -s | tr '[:upper:]' '[:lower:]')"; \
		arch="$$(uname -m)"; \
		case "$$arch" in x86_64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; esac; \
		curl -sSfL "https://github.com/sqlc-dev/sqlc/releases/download/$(SQLC_VERSION)/sqlc_$(SQLC_VERSION:v%=%)_$${os}_$${arch}.tar.gz" | tar -xz -C "$$HOME/.local/bin" sqlc; \
	}
	@$(GOLANGCI_LINT) version | head -n 1
	@sqlc version

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

build-pg:
	$(GO) build -tags pg ./...

tidy:
	$(GO) mod tidy

generate:
	$(GO) run ./tools/gokit db generate

drift: generate
	@out="$$(git status --porcelain -- internal/db/gen)"; if [ -n "$$out" ]; then echo "generated code is out of date — run make generate and commit:"; echo "$$out"; exit 1; fi
