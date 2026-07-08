# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS build

ARG TARGETOS
ARG TARGETARCH
# Optional release stamp for the yago brand version (e.g. 2026.7 or 2026.7-abc123).
# Left empty, the binary reports its compiled-in calendar default; the numeric
# YaCy protocol version is a constant and is never injected here.
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
        -ldflags="-s -w ${VERSION:+-X github.com/D4rk4/yago/yagonode/internal/yagonode.buildVersion=${VERSION}}" \
        -o /out/yago-node \
        ./yagonode/cmd/yago-node

RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot

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

# 8090 peer protocol (P2P), 8080 public search (portal/OpenSearch/Tavily API), 9090 ops/admin
EXPOSE 8090 8080 9090

VOLUME ["/opt/yago/data", "/opt/yago/etc"]

USER nonroot:nonroot

ENTRYPOINT ["/opt/yago/bin/yago-node"]
