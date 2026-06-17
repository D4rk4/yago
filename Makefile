GO ?= go
MODULES := . yacymodel yacyproto
COVER_PROFILE := coverage.out
COVERAGE_MIN ?= 0

.PHONY: fmt fmt-check lint vet arch test cover cover-check build verify

fmt:
	$(GO) tool golangci-lint fmt

fmt-check:
	$(GO) tool golangci-lint fmt --diff

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

verify: fmt-check vet lint arch cover-check build
