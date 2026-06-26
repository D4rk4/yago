# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26 AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

ENV CGO_ENABLED=0 GOFLAGS=-trimpath

WORKDIR /src

COPY go.work go.work.sum ./
COPY go.mod go.sum ./
COPY yacynode/go.mod yacynode/go.sum yacynode/
COPY yacycrawlcontract/go.mod yacycrawlcontract/
COPY yacycrawler/go.mod yacycrawler/
COPY yacymodel/go.mod yacymodel/
COPY yacyproto/go.mod yacyproto/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/yacycrawler \
        ./yacycrawler/cmd/yacycrawler

FROM chromedp/headless-shell:131.0.6778.85

COPY --from=build /out/yacycrawler /usr/local/bin/yacycrawler

ENTRYPOINT ["/usr/local/bin/yacycrawler"]
