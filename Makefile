GO ?= go
MODULES := yagonode yagomodel yagoproto yagocrawlcontract yagoegress yagocrawler
COVER_PROFILE := coverage.out
COVERAGE_MIN ?= 100
COVER_EXCLUDE := /internal/vaulttest/|/test/e2e/|/crawlrpc/
COVERAGE_CHECK := $(CURDIR)/tools/require-exact-coverage

TOOLS_BIN := $(CURDIR)/.toolchain/bin
TOOLS_STAMP := $(TOOLS_BIN)/.installed
GOLANGCI_LINT := $(TOOLS_BIN)/golangci-lint
GO_ARCH_LINT := $(TOOLS_BIN)/go-arch-lint

.PHONY: tools proto-tools proto fmt fmt-check lint vet arch test cover coverage-check-test cover-check build verify e2e e2e-node e2e-crawler e2e-node-image e2e-crawler-image peer-hash

PROTOC ?= protoc
PROTO_MODULE := github.com/D4rk4/yago/yagocrawlcontract
PROTOC_GEN_GO_VERSION ?= v1.36.6
PROTOC_GEN_GO_GRPC_VERSION ?= v1.5.1

E2E_TIMEOUT ?= 25m
E2E_NODE_IMAGE ?= yago-node:e2e
E2E_CRAWLER_IMAGE ?= yago-crawler:e2e
SOURCE_REVISION ?= unknown

E2E_CONTAINER_CLI := $(shell command -v docker >/dev/null 2>&1 && echo docker || echo podman)
E2E_RUNTIME_DIR := $(or $(XDG_RUNTIME_DIR),/run/user/$(shell id -u))
E2E_DOCKER_HOST := $(or $(DOCKER_HOST),$(if $(filter docker,$(E2E_CONTAINER_CLI)),unix:///var/run/docker.sock,unix://$(E2E_RUNTIME_DIR)/podman/podman.sock))
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

coverage-check-test:
	@./tools/require-exact-coverage-test

cover-check:
	@set -e; for m in $(MODULES); do \
		echo "==> cover-check $$m (min $(COVERAGE_MIN)%)"; \
		( cd $$m && \
			if ! $(GO) test -race -coverprofile=$(COVER_PROFILE) ./... > $(COVER_PROFILE).log 2>&1; then \
				cat $(COVER_PROFILE).log; \
				echo "cover-check: test run failed in $$m"; \
				exit 1; \
			fi; \
			grep -vE '$(COVER_EXCLUDE)' $(COVER_PROFILE) > $(COVER_PROFILE).gated; \
			$(COVERAGE_CHECK) $(COVER_PROFILE).gated $(COVERAGE_MIN) || \
				{ echo "coverage below $(COVERAGE_MIN)% in $$m"; exit 1; } ); \
	done

build:
	@set -e; for m in $(MODULES); do \
		echo "==> build $$m"; \
		( cd $$m && $(GO) build ./... ); \
	done

proto-tools:
	GOBIN=$(TOOLS_BIN) $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	GOBIN=$(TOOLS_BIN) $(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

proto:
	cd yagocrawlcontract && PATH="$(TOOLS_BIN):$$PATH" $(PROTOC) \
		--go_out=. --go_opt=module=$(PROTO_MODULE) \
		--go-grpc_out=. --go-grpc_opt=module=$(PROTO_MODULE) \
		-I proto proto/crawlexchange.proto

peer-hash:
	cd yagonode && $(GO) run ./cmd/yago-peer-hash

verify: fmt-check vet lint arch coverage-check-test test cover-check build

e2e-node-image:
	DOCKER_BUILDKIT=1 $(E2E_CONTAINER_CLI) build \
		--build-arg SOURCE_REVISION=$(SOURCE_REVISION) \
		-f yagonode/Dockerfile -t $(E2E_NODE_IMAGE) .

e2e-crawler-image:
	DOCKER_BUILDKIT=1 $(E2E_CONTAINER_CLI) build \
		--build-arg SOURCE_REVISION=$(SOURCE_REVISION) \
		-f yagocrawler/Dockerfile -t $(E2E_CRAWLER_IMAGE) .

e2e-node:
	cd yagonode/test/e2e && GOWORK=off $(E2E_DOCKER_ENV) YAGO_NODE_IMAGE=$(E2E_NODE_IMAGE) \
		$(GO) test -tags e2e -timeout $(E2E_TIMEOUT) -count=1 -v ./...

e2e-crawler:
	cd yagocrawler/test/e2e && GOWORK=off $(E2E_DOCKER_ENV) \
		YAGOCRAWLER_IMAGE=$(E2E_CRAWLER_IMAGE) YAGO_NODE_IMAGE=$(E2E_NODE_IMAGE) \
		$(GO) test -tags e2e -timeout $(E2E_TIMEOUT) -count=1 -v ./...

e2e: e2e-node e2e-crawler
