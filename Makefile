GO ?= go
MODULES := yacynode yacymodel yacyproto yacycrawlcontract yacycrawler
COVER_PROFILE := coverage.out
COVERAGE_MIN ?= 80

.PHONY: fmt fmt-check lint vet arch test cover cover-check build verify e2e e2e-image peer-hash db-migrate

E2E_TIMEOUT ?= 10m
E2E_NODE_IMAGE ?= yacy-rwi-node:e2e

fmt:
	@set -e; for m in $(MODULES); do \
		echo "==> fmt $$m"; \
		( cd $$m && $(GO) tool golangci-lint fmt ); \
	done

fmt-check:
	@set -e; for m in $(MODULES); do \
		echo "==> fmt-check $$m"; \
		( cd $$m && $(GO) tool golangci-lint fmt --diff ); \
	done

lint:
	@set -e; for m in $(MODULES); do \
		echo "==> lint $$m"; \
		( cd $$m && $(GO) tool golangci-lint run ./... ); \
	done

vet:
	@set -e; for m in $(MODULES); do \
		echo "==> vet $$m"; \
		( cd $$m && $(GO) vet ./... ); \
	done

arch:
	$(GO) tool go-arch-lint check

test:
	@set -e; for m in $(MODULES); do \
		echo "==> test $$m"; \
		( cd $$m && $(GO) test -race ./... ); \
	done

cover:
	@set -e; for m in $(MODULES); do \
		echo "==> cover $$m"; \
		( cd $$m && $(GO) test -coverprofile=$(COVER_PROFILE) ./... && \
			$(GO) tool cover -func=$(COVER_PROFILE) ); \
	done

cover-check:
	@set -e; for m in $(MODULES); do \
		echo "==> cover-check $$m (min $(COVERAGE_MIN)%)"; \
		( cd $$m && $(GO) test -race -coverprofile=$(COVER_PROFILE) ./... >/dev/null && \
			stmts=$$(awk 'NR > 1 { sum += $$2 } END { print sum + 0 }' $(COVER_PROFILE)); \
			if [ "$$stmts" -eq 0 ]; then echo "    no statements to cover"; exit 0; fi; \
			total=$$($(GO) tool cover -func=$(COVER_PROFILE) | \
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

db-migrate:
	cd yacynode && $(GO) run ./cmd/yacy-db-migrate -db $(DB)

verify: fmt-check vet lint arch test cover-check build

e2e-image:
	DOCKER_BUILDKIT=1 docker build -t $(E2E_NODE_IMAGE) .

e2e:
	cd yacynode/test/e2e && GOWORK=off YACY_NODE_IMAGE=$(E2E_NODE_IMAGE) \
		$(GO) test -tags e2e -timeout $(E2E_TIMEOUT) -count=1 -v ./...
