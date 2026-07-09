# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

ENV CGO_ENABLED=0 GOFLAGS=-trimpath

WORKDIR /src

COPY go.work go.work.sum ./
COPY --parents libraries/*/go.mod libraries/*/go.sum services/*/go.mod services/*/go.sum ./

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
        ./services/yacycrawler/cmd/yacycrawler

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/yacycrawler /usr/local/bin/yacycrawler

ENTRYPOINT ["/usr/local/bin/yacycrawler"]
