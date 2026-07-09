GO ?= go
PYTHON ?= python3
COVERAGE_MIN ?= 80

GO_MODULES := services/yacynode libraries/yacymodel libraries/yacyproto libraries/yacycrawlcontract libraries/bytesize libraries/serviceruntime services/yacycrawler services/yacytextindexer services/yacyvisitcrawl services/renderproxy
PY_MODULES := plugins/searxng/searxng-result-router plugins/searxng/searxng-crawled-text-search

COVER_PROFILE := coverage.out
COVER_EXCLUDE := /internal/vaulttest/|/test/e2e/|/internal/cdprender/

TOOLS_BIN := $(CURDIR)/.toolchain/bin
TOOLS_STAMP := $(TOOLS_BIN)/.installed
GOLANGCI_LINT := $(TOOLS_BIN)/golangci-lint
GO_ARCH_LINT := $(TOOLS_BIN)/go-arch-lint
RUFF := $(TOOLS_BIN)/ruff

PY_VENV_STAMPS := $(foreach m,$(PY_MODULES),$(m)/.venv/.installed)

define for_each_go
set -e; for m in $(GO_MODULES); do echo "==> $(1) $$m"; ( cd $$m && $(2) ); done
endef

define for_each_py
set -e; for m in $(PY_MODULES); do echo "==> $(1) $$m"; ( cd $$m && $(2) ); done
endef

.PHONY: tools \
	fmt fmt-go fmt-py \
	fmt-check fmt-check-go fmt-check-py \
	lint lint-go lint-py \
	vet arch \
	test test-go test-py \
	cover cover-go cover-py \
	cover-check cover-check-go cover-check-py \
	build verify peer-hash \
	e2e e2e-images

fmt:         fmt-go fmt-py
fmt-check:   fmt-check-go fmt-check-py
lint:        lint-go lint-py
test:        test-go test-py
cover:       cover-go cover-py
cover-check: cover-check-go cover-check-py
build:       build-go
verify:      fmt-check vet lint arch test cover-check build

$(TOOLS_STAMP): tools/install tools/tools.lock
	./tools/install
	@touch $@

tools: $(TOOLS_STAMP)

$(PY_VENV_STAMPS): %/.venv/.installed: %/requirements-dev.txt
	$(PYTHON) -m venv $*/.venv
	$*/.venv/bin/pip install --quiet -r $*/requirements-dev.txt
	@touch $@

# ---- Go stack ----

fmt-go: $(TOOLS_STAMP)
	@$(call for_each_go,fmt,$(GOLANGCI_LINT) fmt)

fmt-check-go: $(TOOLS_STAMP)
	@$(call for_each_go,fmt-check,$(GOLANGCI_LINT) fmt --diff)

lint-go: $(TOOLS_STAMP)
	@$(call for_each_go,lint,$(GOLANGCI_LINT) run ./...)

vet:
	@$(call for_each_go,vet,$(GO) vet ./...)

arch: $(TOOLS_STAMP)
	@$(call for_each_go,arch,$(GO_ARCH_LINT) check)

test-go:
	@$(call for_each_go,test,$(GO) test -race ./...)

build-go:
	@$(call for_each_go,build,$(GO) build ./...)

cover-go:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> cover $$m"; \
		( cd $$m && $(GO) test -coverprofile=$(COVER_PROFILE) ./... && \
			grep -vE '$(COVER_EXCLUDE)' $(COVER_PROFILE) > $(COVER_PROFILE).gated; \
			$(GO) tool cover -func=$(COVER_PROFILE).gated ); \
	done

cover-check-go:
	@set -e; for m in $(GO_MODULES); do \
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

# ---- Python stack ----

fmt-py: $(TOOLS_STAMP)
	@$(call for_each_py,fmt,$(RUFF) format .)

fmt-check-py: $(TOOLS_STAMP)
	@$(call for_each_py,fmt-check,$(RUFF) format --check .)

lint-py: $(TOOLS_STAMP)
	@$(call for_each_py,lint,$(RUFF) check .)

test-py: $(PY_VENV_STAMPS)
	@$(call for_each_py,test,.venv/bin/python -m pytest -q)

cover-py: $(PY_VENV_STAMPS)
	@$(call for_each_py,cover,.venv/bin/python -m pytest -q --cov --cov-report=term-missing)

cover-check-py: $(PY_VENV_STAMPS)
	@$(call for_each_py,cover-check,.venv/bin/python -m pytest -q --cov --cov-fail-under=$(COVERAGE_MIN))

# ---- misc ----

peer-hash:
	cd services/yacynode && $(GO) run ./cmd/yacy-peer-hash

# ---- e2e ----

E2E_TIMEOUT ?= 10m

E2E_CONTAINER_CLI := $(shell command -v docker >/dev/null 2>&1 && echo docker || \
	(command -v podman >/dev/null 2>&1 && echo podman || echo "distrobox-host-exec podman"))
E2E_RUNTIME_DIR := $(or $(XDG_RUNTIME_DIR),/run/user/$(shell id -u))
E2E_DOCKER_HOST := $(or $(DOCKER_HOST),unix://$(E2E_RUNTIME_DIR)/podman/podman.sock)
E2E_DOCKER_ENV := DOCKER_HOST=$(E2E_DOCKER_HOST) TESTCONTAINERS_RYUK_DISABLED=true

# Modules that build a docker image for e2e testing, and the tag each produces.
E2E_IMAGE_MODULES := yacynode yacycrawler yacytextindexer yacyvisitcrawl renderproxy

E2E_PATH_yacynode        := services/yacynode
E2E_PATH_yacycrawler     := services/yacycrawler
E2E_PATH_yacytextindexer := services/yacytextindexer
E2E_PATH_yacyvisitcrawl  := services/yacyvisitcrawl
E2E_PATH_renderproxy     := services/renderproxy

E2E_IMAGE_yacynode        := yacy-rwi-node:e2e
E2E_IMAGE_yacycrawler     := yacy-rwi-crawler:e2e
E2E_IMAGE_yacytextindexer := yacy-rwi-textindexer:e2e
E2E_IMAGE_yacyvisitcrawl  := yacyvisitcrawl:e2e
E2E_IMAGE_renderproxy     := renderproxy:e2e

define e2e_image_rule
e2e-$(1)-image:
	DOCKER_BUILDKIT=1 $$(E2E_CONTAINER_CLI) build -f $$(E2E_PATH_$(1))/Dockerfile -t $$(E2E_IMAGE_$(1)) .
endef
$(foreach m,$(E2E_IMAGE_MODULES),$(eval $(call e2e_image_rule,$(m))))

e2e-images: $(foreach m,$(E2E_IMAGE_MODULES),e2e-$(m)-image)

# Modules that own a test/e2e suite, and the images each suite needs.
E2E_SUITE_MODULES := yacynode yacycrawler yacytextindexer searxng-result-router searxng-crawled-text-search renderproxy

E2E_PATH_searxng-result-router         := plugins/searxng/searxng-result-router
E2E_PATH_searxng-crawled-text-search   := plugins/searxng/searxng-crawled-text-search

E2E_ENV_yacynode                       := YACY_NODE_IMAGE=$(E2E_IMAGE_yacynode)
E2E_ENV_yacycrawler                    := YACYCRAWLER_IMAGE=$(E2E_IMAGE_yacycrawler)
E2E_ENV_yacytextindexer                := YACY_NODE_IMAGE=$(E2E_IMAGE_yacynode) YACYCRAWLER_IMAGE=$(E2E_IMAGE_yacycrawler) YACYTEXTINDEXER_IMAGE=$(E2E_IMAGE_yacytextindexer)
E2E_ENV_searxng-result-router          := YACYVISITCRAWL_IMAGE=$(E2E_IMAGE_yacyvisitcrawl)
E2E_ENV_searxng-crawled-text-search    :=
E2E_ENV_renderproxy                    := RENDERPROXY_IMAGE=$(E2E_IMAGE_renderproxy)

define e2e_suite_rule
e2e-$(1):
	cd $$(E2E_PATH_$(1))/test/e2e && GOWORK=off $$(E2E_DOCKER_ENV) $$(E2E_ENV_$(1)) \
		$$(GO) test -tags e2e -timeout $$(E2E_TIMEOUT) -count=1 -v ./...
endef
$(foreach m,$(E2E_SUITE_MODULES),$(eval $(call e2e_suite_rule,$(m))))

e2e: $(foreach m,$(E2E_SUITE_MODULES),e2e-$(m))
