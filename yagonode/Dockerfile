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
        -ldflags="-s -w -X github.com/D4rk4/yago/yagonode/internal/yagonode.buildVersion=${VERSION}" \
        -o /out/yago-node \
        ./yagonode/cmd/yago-node

RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:b7bb25d9f7c31d2bdd1982feb4dafcaf137703c7075dbe2febb41c24212b946f

# Shared /opt/yago layout (OPS-04): binaries under /opt/yago/bin, mutable data
# under /opt/yago/data, config under /opt/yago/etc — the same layout the deb and
# systemd deployments use. Migrating a pre-layout deployment: see
# deploy/README.md ("Container layout migration").
ENV YAGO_DATA_DIR=/opt/yago/data \
    YAGO_PEER_ADDR=:8090 \
    YAGO_OPS_ADDR=:9090 \
    PATH=/opt/yago/bin

COPY --from=build --chown=nonroot:nonroot /out/data /opt/yago/data
COPY --from=build --chown=nonroot:nonroot /out/data /opt/yago/etc
COPY --from=build /out/yago-node /opt/yago/bin/yago-node

ARG SOURCE_REVISION=unknown

LABEL org.opencontainers.image.revision="${SOURCE_REVISION}" \
    org.opencontainers.image.source="https://github.com/D4rk4/yago"

# 8090 peer protocol (P2P), 8080 public search (portal/OpenSearch/Tavily API), 9090 ops/admin
EXPOSE 8090 8080 9090

VOLUME ["/opt/yago/data", "/opt/yago/etc"]

USER nonroot:nonroot

ENTRYPOINT ["/opt/yago/bin/yago-node"]
