# 0044. Alpine browser crawler runtime

Date: 2026-07-11

## Status

Accepted

## Context

The crawler container needs Firefox ESR for its optional Marionette slow path,
CA trust roots for TLS, fonts for stable page rendering, and the `nice` and
`ionice` commands used by its entrypoint. The Debian 13 runtime accumulated
unfixed HIGH and CRITICAL findings in packages unrelated to the static Go
crawler binary, so it could not pass the mandatory image gate.

## Decision

Use the Docker Official Image `alpine:3.24.1`, pinned to manifest digest
`sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b`,
for the crawler runtime. Install the Alpine v3.24 packages
`firefox-esr=140.12.0-r0`, `ca-certificates=20260611-r0`, and
`font-liberation=2.1.5-r2`. These versions are available for both release
architectures, `amd64` and `arm64`.

Alpine Linux is distributed under multiple open-source licenses. Firefox ESR is
MPL-2.0 with GPL and LGPL components, CA certificates are MPL-2.0 and MIT, and
Liberation fonts are OFL-1.1. The statically linked Go crawler remains unchanged.

## Alternatives

Keeping Debian 13 was rejected because its current package tree fails the
mandatory HIGH/CRITICAL scan with no fixed package versions available. Debian
12 reduces but does not eliminate those findings. Removing Firefox was rejected
because it would remove the container's supported browser fallback. Copying
browser files into a package-less image was rejected because it would hide
vulnerability inventory from scanners.

## Consequences

The crawler keeps the same binary path, non-root UID, entrypoint, Firefox
discovery, browser policy, and operator configuration. The image uses musl and
BusyBox for its runtime utilities, while the crawler itself remains a static
CGO-disabled binary. Package and base updates are explicit source changes and
must pass the image scan and container e2e suite.

## Sources

- [Alpine Docker Official Image](https://hub.docker.com/_/alpine)
- [Alpine Firefox ESR package](https://pkgs.alpinelinux.org/package/v3.24/community/x86_64/firefox-esr)
- [Alpine Firefox installation](https://wiki.alpinelinux.org/wiki/Firefox)
