# 15. Hash admin passwords with Argon2id

Date: 2026-07-03

## Status

Accepted

## Context

SEC-01 adds administrator authentication for the node operations surface. The
operator password must be stored as a modern, memory-hard password hash rather
than a fast general-purpose digest, so that a leaked credential store cannot be
brute-forced cheaply. The Go standard library has no password-hashing primitive:
`bcrypt`, `scrypt`, and `argon2` all live under `golang.org/x/crypto`, which is
not yet a direct dependency of the node module. Adding a dependency requires an
ADR per `AGENTS.md`.

The SEC-01 roadmap task already named the preferred primitive: Argon2id via
`golang.org/x/crypto/argon2` with stored parameters. Argon2id is the current
OWASP-recommended default for password storage, is memory-hard (resisting GPU and
ASIC attacks better than bcrypt), and won the Password Hashing Competition.

## Decision

We add `golang.org/x/crypto` and hash admin passwords with Argon2id
(`golang.org/x/crypto/argon2`).

- Each password is hashed with a fresh 16-byte random salt from `crypto/rand`.
- The encoded credential is the standard PHC string
  `$argon2id$v=19$m=<memoryKiB>,t=<iterations>,p=<parallelism>$<b64salt>$<b64hash>`,
  so the parameters travel with the hash and can be tuned or migrated later
  without a schema change.
- Verification parses the stored parameters, recomputes the hash over the
  supplied password, and compares with `crypto/subtle.ConstantTimeCompare`.
- Default parameters follow the OWASP Argon2id guidance: 64 MiB memory, 3
  iterations, parallelism 2, 32-byte key. They are constants in one place so a
  future ADR can raise them.

`golang.org/x/crypto` is already an indirect dependency of the gRPC and protobuf
stacks pulled in by ADR-0014, so this promotes an existing transitive module to a
direct one rather than introducing a brand-new supply-chain surface.

## Consequences

- New direct dependency pinned in the node `go.mod`: `golang.org/x/crypto`
  (only its `argon2` and, transitively, `blake2b` packages are used).
- The `.go-arch-lint.yml` gains a `crypto` vendor scoped to
  `golang.org/x/crypto/**`, usable only by the new `adminauth` component.
- Password verification is intentionally CPU- and memory-costly (~64 MiB per
  call). Login is rate-limited (SEC-01) so this cost cannot be turned into a
  cheap amplification vector, and unauthenticated callers cannot reach the
  verifier except through the throttled login path.
- Stored hashes are self-describing; raising the cost parameters later re-hashes
  on next successful login rather than requiring a migration.
