# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

ENV CGO_ENABLED=0 GOFLAGS=-trimpath

WORKDIR /src

COPY go.work go.work.sum ./
COPY yacynode/go.mod yacynode/go.sum yacynode/
COPY yacycrawlcontract/go.mod yacycrawlcontract/
COPY yacycrawler/go.mod yacycrawler/
COPY yacymodel/go.mod yacymodel/
COPY yacyproto/go.mod yacyproto/
COPY yacytextindexer/go.mod yacytextindexer/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/yacy-rwi-node \
        ./yacynode/cmd/yacy-rwi-node

RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot

ENV YACY_DATA_DIR=/data \
    YACY_PEER_ADDR=:8090 \
    YACY_OPS_ADDR=:9090

COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY --from=build /out/yacy-rwi-node /usr/local/bin/yacy-rwi-node

EXPOSE 8090 9090

VOLUME ["/data"]

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/yacy-rwi-node"]
