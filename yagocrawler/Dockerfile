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
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/yagocrawler \
        ./yagocrawler/cmd/yagocrawler

FROM docker.io/chromedp/headless-shell:148.0.7778.97 AS browser

WORKDIR /runtime

RUN set -eux; \
    mkdir -p etc usr var; \
    cp -a /headless-shell headless-shell; \
    cp -a /lib lib; \
    if [ -e /lib64 ]; then cp -a /lib64 lib64; fi; \
    cp -a /usr/lib usr/lib; \
    if [ -e /usr/lib64 ]; then cp -a /usr/lib64 usr/lib64; fi; \
    cp -a /etc/fonts etc/fonts; \
    cp -a /etc/nsswitch.conf etc/nsswitch.conf; \
    mkdir -m 1777 tmp var/tmp

FROM scratch

ENV HOME=/tmp XDG_CACHE_HOME=/tmp/.cache PATH=/usr/local/bin:/headless-shell

COPY --from=browser /runtime/ /
COPY --from=build /out/yagocrawler /usr/local/bin/yagocrawler

USER 65534:65534

ENTRYPOINT ["/usr/local/bin/yagocrawler"]
