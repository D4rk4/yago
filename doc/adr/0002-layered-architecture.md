# 2. Separate HTTP handlers, domain logic, and adapters

Date: 2026-06-17

## Status

Accepted

## Context

This is a small node, and we want to keep it that way. The risk is the usual drift: HTTP
handlers accumulate domain logic, and the domain accumulates transport and validation
concerns. We want handlers to stay thin and the domain to only ever see input it can trust.

## Decision

The node is split into three packages. `api` is the HTTP boundary: it parses requests,
validates them against the YaCy models in `yacymodel`, and hands the validated models to the
domain. `core` holds the domain logic; it operates only on already-validated models and defines
the ports its adapters implement. `infrastructure` implements those ports — storage and the
peer-facing clients — validating any models it ingests from peers. Dependencies point inward:
`api` and `infrastructure` depend on `core` and do not depend on each other; all three build on
the shared `yacymodel` models. `cmd` is the only place they are wired together.

The dependency rules are declared and enforced in `.go-arch-lint.yml`.

## Consequences

Handlers carry no domain logic, and domain code can assume valid input and never touches HTTP.
Validation is defined once in `yacymodel` and runs at the boundaries that admit external data.
The boundaries are enforced automatically, so violations fail the build instead of
accumulating. The cost is three packages instead of one — modest, and it buys the separation
above.
