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

Use `github.com/testcontainers/testcontainers-go` in two isolated Go modules,
`yagonode/test/e2e` and `yago-crawler/test/e2e`, behind the `e2e` build tag and a dedicated
`make e2e` target. The node module manages native-node fleets and a real YaCy server. The crawler
module manages the node, crawler, and deterministic origin fixtures used for crawl and ingest
workflows.

Both modules are isolated from the production modules so testcontainers and its transitive Docker
client dependencies never enter the runtime dependency graph, the `make verify` gate, or the
architecture linter scope. The node module imports only published model and protocol boundaries for
wire encoding and parsing. Product processes under test run as containers built from
`yagonode/Dockerfile` and `yago-crawler/Dockerfile`.

The crawler module includes a successful learned-ranking promotion workflow. It ingests 66
documents for 22 query clusters through the node's crawl transport, stores graded judgments, and
uses 1 training, 1 development, and 20 test clusters. The test requires held-out improvement,
promotion and activation of the candidate model, an explanation that names the learned evidence,
and a public-search top result that changes from the deliberately weak document to the relevant
document.

## Considered alternatives

Shelling out to the `docker` CLI from `os/exec` was considered to avoid a new dependency. It was
rejected because container lifecycle, network creation, log capture, and cleanup would be reimplemented
by hand, and a leaked container or network on a failed test would be likely.

A hand-rolled Docker API client over the Docker socket was considered. It was rejected for the same
reason at a lower level of abstraction, with more code to maintain than the CLI option.

## Consequences

End-to-end interoperability, crawl-to-index delivery, and ranking promotion are verified in real
product containers on demand. The testcontainers dependency is confined to the two e2e modules;
production modules, release artifacts, and `make verify` are unaffected. Running `make e2e`
requires a working Docker daemon and network access for any image that is not already local.
