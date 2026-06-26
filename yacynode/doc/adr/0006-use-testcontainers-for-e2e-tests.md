# 6. Use testcontainers-go for end-to-end tests

Date: 2026-06-18

## Status

Accepted

## Context

The node claims YaCy DHT interoperability. Unit tests exercise the wire encoders and handlers in
isolation, but they cannot prove that a real, unmodified YaCy peer discovers the node, promotes it to
`senior`, and originates a DHT index transfer into it. That behaviour depends on YaCy's own runtime
gates and can only be observed by running an actual YaCy server against a running node.

These tests need to start the node image and a real `yacy/yacy_search_server` container on a shared
network, drive them over HTTP, and tear everything down deterministically. They are slow, require a
Docker daemon, and must never run as part of the standard quality gate.

## Decision

Use `github.com/testcontainers/testcontainers-go` to manage the YaCy and node containers in a separate
`test/e2e` Go module, behind the `e2e` build tag and a dedicated `make e2e` target.

The module is isolated from the main module so testcontainers and its transitive Docker client
dependencies never enter the runtime dependency graph, the `make verify` gate, or the architecture
linter scope. The e2e module imports only the published `yacymodel` and `yacyproto` modules for wire
encoding and parsing; the node under test runs as a container built from `yacynode/Dockerfile`.

## Considered alternatives

Shelling out to the `docker` CLI from `os/exec` was considered to avoid a new dependency. It was
rejected because container lifecycle, network creation, log capture, and cleanup would be reimplemented
by hand, and a leaked container or network on a failed test would be likely.

A hand-rolled Docker API client over the Docker socket was considered. It was rejected for the same
reason at a lower level of abstraction, with more code to maintain than the CLI option.

## Consequences

End-to-end interoperability is verified against a real YaCy server on demand. The testcontainers
dependency is confined to `test/e2e/go.mod`; the main module, its release artifact, and `make verify`
are unaffected. Running `make e2e` requires a working Docker daemon and network access to pull the YaCy
image.
