# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=

ENV CGO_ENABLED=0 GOFLAGS=-trimpath

WORKDIR /src

COPY go.work go.work.sum ./
COPY yagonode/go.mod yagonode/go.sum yagonode/
COPY yagocrawlcontract/go.mod yagocrawlcontract/
COPY yagocrawler/go.mod yagocrawler/
COPY yagoegress/go.mod yagoegress/
COPY yagomodel/go.mod yagomodel/
COPY yagoproto/go.mod yagoproto/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
        -ldflags="-s -w ${VERSION:+-X main.version=${VERSION}}" \
        -o /out/yagocrawler \
        ./yagocrawler/cmd/yagocrawler

# The crawler's slow-path browser is headless Firefox, driven over Marionette.
# Firefox is not distributed as a scratch-friendly bundle the way Chrome's
# headless-shell was, so the runtime image is debian-slim with firefox-esr
# installed: apt pulls Firefox's own shared-library and fontconfig dependencies,
# and ca-certificates supplies the trust roots for outbound TLS. The static
# crawler binary runs unchanged on this base.
FROM docker.io/library/debian:trixie-slim

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends firefox-esr ca-certificates fonts-liberation; \
    rm -rf /var/lib/apt/lists/*

# Shared /opt/yago layout (OPS-04): the crawler is stateless (no data volume),
# so only the binary moves under /opt/yago/bin. HOME must be writable for the
# throwaway Firefox profile; the crawler discovers firefox-esr on PATH, so
# YAGOCRAWLER_BROWSER_PATH stays empty in the container.
ENV HOME=/tmp XDG_CACHE_HOME=/tmp/.cache PATH=/opt/yago/bin:/usr/bin

COPY --from=build /out/yagocrawler /opt/yago/bin/yagocrawler

USER 65534:65534

ENTRYPOINT ["/opt/yago/bin/yagocrawler"]
