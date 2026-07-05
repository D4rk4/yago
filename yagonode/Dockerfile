# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

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
        -ldflags="-s -w -X github.com/D4rk4/yago/yagonode/internal/yagonode.version=${VERSION}" \
        -o /out/yago-node \
        ./yagonode/cmd/yago-node

RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot

ENV YAGO_DATA_DIR=/data \
    YAGO_PEER_ADDR=:8090 \
    YAGO_OPS_ADDR=:9090

COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY --from=build /out/yago-node /usr/local/bin/yago-node

# 8090 peer protocol (P2P), 8080 public search (portal/OpenSearch/Tavily API), 9090 ops/admin
EXPOSE 8090 8080 9090

VOLUME ["/data"]

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/yago-node"]
