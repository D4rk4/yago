# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26@sha256:079e59808d2d252516e27e3f3a9c003740dee7f75e55aa71528766d52bcfc16a AS build

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
    set -eu; \
    printf '%s\n' "$VERSION" | grep -Eq '^(v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)|[0-9]{4}\.[0-9]{2}\.[0-9]{2}-dev)$'; \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/yagocrawler \
        ./yagocrawler/cmd/yagocrawler

FROM docker.io/library/alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache \
    ca-certificates=20260611-r0 \
    firefox-esr=140.12.0-r0 \
    font-liberation=2.1.5-r2

ENV HOME=/tmp XDG_CACHE_HOME=/tmp/.cache PATH=/opt/yago/bin:/usr/bin:/bin

COPY --from=build /out/yagocrawler /opt/yago/bin/yagocrawler

ARG SOURCE_REVISION=unknown

LABEL org.opencontainers.image.revision="${SOURCE_REVISION}" \
    org.opencontainers.image.source="https://github.com/D4rk4/yago"

USER 65534:65534

ENTRYPOINT ["ionice", "-c", "2", "-n", "6", "nice", "-n", "5", "/opt/yago/bin/yagocrawler"]
