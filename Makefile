GO ?= go
MODULES := yacynode yacymodel yacyproto yacycrawlcontract yacycrawler
COVER_PROFILE := coverage.out
COVERAGE_MIN ?= 80
COVER_EXCLUDE := /internal/vaulttest/|/test/e2e/

TOOLS_BIN := $(CURDIR)/.toolchain/bin
TOOLS_STAMP := $(TOOLS_BIN)/.installed
GOLANGCI_LINT := $(TOOLS_BIN)/golangci-lint
GO_ARCH_LINT := $(TOOLS_BIN)/go-arch-lint

.PHONY: tools fmt fmt-check lint vet arch test cover cover-check build verify e2e e2e-node e2e-crawler e2e-node-image e2e-crawler-image peer-hash

E2E_TIMEOUT ?= 10m
E2E_NODE_IMAGE ?= yacy-rwi-node:e2e
E2E_CRAWLER_IMAGE ?= yacy-rwi-crawler:e2e

E2E_CONTAINER_CLI := $(shell command -v docker >/dev/null 2>&1 && echo docker || echo podman)
E2E_RUNTIME_DIR := $(or $(XDG_RUNTIME_DIR),/run/user/$(shell id -u))
E2E_DOCKER_HOST := $(or $(DOCKER_HOST),unix://$(E2E_RUNTIME_DIR)/podman/podman.sock)
E2E_DOCKER_ENV := DOCKER_HOST=$(E2E_DOCKER_HOST) TESTCONTAINERS_RYUK_DISABLED=true

$(TOOLS_STAMP): tools/install tools/tools.lock
	./tools/install
	@touch $@

tools: $(TOOLS_STAMP)

fmt: $(TOOLS_STAMP)
	@set -e; for m in $(MODULES); do \
		echo "==> fmt $$m"; \
		( cd $$m && $(GOLANGCI_LINT) fmt ); \
	done

fmt-check: $(TOOLS_STAMP)
	@set -e; for m in $(MODULES); do \
		echo "==> fmt-check $$m"; \
		( cd $$m && $(GOLANGCI_LINT) fmt --diff ); \
	done

lint: $(TOOLS_STAMP)
	@set -e; for m in $(MODULES); do \
		echo "==> lint $$m"; \
		( cd $$m && $(GOLANGCI_LINT) run ./... ); \
	done

vet:
	@set -e; for m in $(MODULES); do \
		echo "==> vet $$m"; \
		( cd $$m && $(GO) vet ./... ); \
	done

arch: $(TOOLS_STAMP)
	@set -e; for m in $(MODULES); do \
		echo "==> arch $$m"; \
		( cd $$m && $(GO_ARCH_LINT) check ); \
	done

test:
	@set -e; for m in $(MODULES); do \
		echo "==> test $$m"; \
		( cd $$m && $(GO) test -race ./... ); \
	done

cover:
	@set -e; for m in $(MODULES); do \
		echo "==> cover $$m"; \
		( cd $$m && $(GO) test -coverprofile=$(COVER_PROFILE) ./... && \
			grep -vE '$(COVER_EXCLUDE)' $(COVER_PROFILE) > $(COVER_PROFILE).gated; \
			$(GO) tool cover -func=$(COVER_PROFILE).gated ); \
	done

cover-check:
	@set -e; for m in $(MODULES); do \
		echo "==> cover-check $$m (min $(COVERAGE_MIN)%)"; \
		( cd $$m && $(GO) test -race -coverprofile=$(COVER_PROFILE) ./... >/dev/null && \
			grep -vE '$(COVER_EXCLUDE)' $(COVER_PROFILE) > $(COVER_PROFILE).gated; \
			stmts=$$(awk 'NR > 1 { sum += $$2 } END { print sum + 0 }' $(COVER_PROFILE).gated); \
			if [ "$$stmts" -eq 0 ]; then echo "    no statements to cover"; exit 0; fi; \
			total=$$($(GO) tool cover -func=$(COVER_PROFILE).gated | \
				awk '/^total:/ { gsub(/%/, "", $$3); print $$3 }'); \
			echo "    total: $${total:-0}%"; \
			awk -v c="$${total:-0}" -v min="$(COVERAGE_MIN)" \
				'BEGIN { if (c + 0 < min + 0) { exit 1 } }' || \
				{ echo "coverage $${total:-0}% below $(COVERAGE_MIN)% in $$m"; exit 1; } ); \
	done

build:
	@set -e; for m in $(MODULES); do \
		echo "==> build $$m"; \
		( cd $$m && $(GO) build ./... ); \
	done

peer-hash:
	cd yacynode && $(GO) run ./cmd/yacy-peer-hash

verify: fmt-check vet lint arch test cover-check build

e2e-node-image:
	DOCKER_BUILDKIT=1 $(E2E_CONTAINER_CLI) build -f yacynode/Dockerfile -t $(E2E_NODE_IMAGE) .

e2e-crawler-image:
	DOCKER_BUILDKIT=1 $(E2E_CONTAINER_CLI) build -f yacycrawler/Dockerfile -t $(E2E_CRAWLER_IMAGE) .

e2e-node:
	cd yacynode/test/e2e && GOWORK=off $(E2E_DOCKER_ENV) YACY_NODE_IMAGE=$(E2E_NODE_IMAGE) \
		$(GO) test -tags e2e -timeout $(E2E_TIMEOUT) -count=1 -v ./...

e2e-crawler:
	cd yacycrawler/test/e2e && GOWORK=off $(E2E_DOCKER_ENV) YACYCRAWLER_IMAGE=$(E2E_CRAWLER_IMAGE) \
		$(GO) test -tags e2e -timeout $(E2E_TIMEOUT) -count=1 -v ./...

e2e: e2e-node e2e-crawler
