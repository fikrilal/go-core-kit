SHELL := /bin/bash

GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= v2.14.0
SQLC_VERSION ?= v1.31.1
OAPI_CODEGEN_VERSION ?= v2.8.0
VACUUM_VERSION ?= v0.33.0
GOOSE_VERSION ?= v3.28.0

.PHONY: verify verify-pg fmt fmt-check vet lint test build build-pg tidy tools generate drift scaffold-smoke

verify:
	$(GO) run ./tools/gokit verify --profile full

verify-pg:
	$(GO) run ./tools/gokit verify --profile pg

tools:
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b "$$HOME/.local/bin" $(GOLANGCI_LINT_VERSION)
	@command -v sqlc >/dev/null 2>&1 || { \
		os="$$(uname -s | tr '[:upper:]' '[:lower:]')"; \
		arch="$$(uname -m)"; \
		case "$$arch" in x86_64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; esac; \
		curl -sSfL "https://github.com/sqlc-dev/sqlc/releases/download/$(SQLC_VERSION)/sqlc_$(SQLC_VERSION:v%=%)_$${os}_$${arch}.tar.gz" | tar -xz -C "$$HOME/.local/bin" sqlc; \
	}
	@command -v oapi-codegen >/dev/null 2>&1 || GOBIN="$$HOME/.local/bin" $(GO) install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION)
	@command -v vacuum >/dev/null 2>&1 || { \
		os="$$(uname -s | tr '[:upper:]' '[:lower:]')"; \
		arch="$$(uname -m)"; \
		case "$$arch" in x86_64) arch=x86_64 ;; aarch64 | arm64) arch=arm64 ;; esac; \
		curl -sSfL "https://github.com/daveshanley/vacuum/releases/download/$(VACUUM_VERSION)/vacuum_$(VACUUM_VERSION:v%=%)_$${os}_$${arch}.tar.gz" | tar -xz -C "$$HOME/.local/bin" vacuum; \
	}
	@command -v goose >/dev/null 2>&1 || GOBIN="$$HOME/.local/bin" $(GO) install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
	@$(GOLANGCI_LINT) version | head -n 1
	@sqlc version
	@oapi-codegen --version
	@vacuum version
	@goose --version

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
	$(GO) run ./tools/gokit contract generate

drift:
	$(GO) run ./tools/gokit drift check

# scaffold-smoke proves scaffolded output passes the gate in a throwaway copy.
scaffold-smoke:
	bash tools/gokit/scaffold-smoke.sh
