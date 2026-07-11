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

FROM docker.io/library/alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache \
    ca-certificates=20260611-r0 \
    firefox-esr=140.12.0-r0 \
    font-liberation=2.1.5-r2

# Shared /opt/yago layout (OPS-04): the crawler is stateless (no data volume),
# so only the binary moves under /opt/yago/bin. HOME must be writable for the
# throwaway Firefox profile; the crawler discovers firefox-esr on PATH, so
# YAGOCRAWLER_BROWSER_PATH stays empty in the container.
ENV HOME=/tmp XDG_CACHE_HOME=/tmp/.cache PATH=/opt/yago/bin:/usr/bin:/bin

COPY --from=build /out/yagocrawler /opt/yago/bin/yagocrawler

USER 65534:65534

# Run the crawler at the lowest scheduling priority so it yields CPU and disk to
# a co-located node (PERF-PRIO-01). ionice best-effort/7 and nice 19 are the
# lowest priorities an unprivileged container process can set without extra
# capabilities (the idle I/O class needs CAP_SYS_ADMIN on some kernels; systemd,
# which runs privileged setup, uses idle instead). Both tools exec their target,
# so yagocrawler stays PID 1 and still receives SIGTERM for graceful shutdown.
ENTRYPOINT ["ionice", "-c", "2", "-n", "7", "nice", "-n", "19", "/opt/yago/bin/yagocrawler"]
