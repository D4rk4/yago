# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26.6@sha256:0d1d3a794be25f809dd2cb3160d8c73276c4056a9f8242a138e908ddeee7b6b6 AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=

ENV CGO_ENABLED=0 GOFLAGS=-trimpath

WORKDIR /src

COPY go.work go.work.sum ./
COPY yagonode/go.mod yagonode/go.sum yagonode/
COPY yagocrawlcontract/go.mod yagocrawlcontract/
COPY yago-crawler/go.mod yago-crawler/
COPY yagoegress/go.mod yagoegress/
COPY yagomodel/go.mod yagomodel/
COPY yagoproto/go.mod yagoproto/
COPY deploy/download-go-modules.sh deploy/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    sh deploy/download-go-modules.sh

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eu; \
    printf '%s\n' "$VERSION" | grep -Eq '^(v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)|[0-9]{4}\.[0-9]{2}\.[0-9]{2}-dev)$'; \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/yago-crawler \
        ./yago-crawler/cmd/yago-crawler

FROM docker.io/library/alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache \
    ca-certificates=20260611-r0 \
    firefox-esr=140.12.0-r0 \
    font-liberation=2.1.5-r2 \
    libcrypto3=3.5.8-r0 \
    libssl3=3.5.8-r0

ENV HOME=/tmp \
    XDG_CACHE_HOME=/tmp/.cache \
    PATH=/opt/yago/bin:/usr/bin:/bin \
    YAGO_DATA_DIR=/opt/yago/data

RUN mkdir -p /opt/yago/data && \
    chown 65534:65534 /opt/yago/data && \
    chmod 0750 /opt/yago/data

COPY --from=build /out/yago-crawler /opt/yago/bin/yago-crawler

ARG SOURCE_REVISION=unknown

LABEL org.opencontainers.image.revision="${SOURCE_REVISION}" \
    org.opencontainers.image.source="https://github.com/D4rk4/yago"

VOLUME ["/opt/yago/data"]

USER 65534:65534

ENTRYPOINT ["ionice", "-c", "2", "-n", "6", "nice", "-n", "5", "/opt/yago/bin/yago-crawler"]
