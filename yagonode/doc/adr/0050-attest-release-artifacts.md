# 50. Attest release artifacts with GitHub-hosted provenance

Date: 2026-07-16

## Status

Accepted

## Context

The tag workflow publishes architecture-specific tarballs, Debian packages, and
RPM packages. A package checksum detects a changed download only when the
operator already has a trusted checksum. It does not independently bind the
artifact to the repository, tag workflow, and source commit that produced it.

## Decision

Use `actions/attest` release `v4.1.1`, pinned to commit
`a1948c3f048ba23858d222213b7c278aabede763`, after package construction and the
applicable amd64 package smoke tests, and before artifact upload. The action is
MIT-licensed and produces
GitHub-hosted, Sigstore-signed in-toto provenance for every tarball, Debian
package, and RPM package.

The build job receives only the repository-read, OIDC, attestation-write, and
artifact-metadata permissions documented by that exact action version. The
release job downloads the same bytes and verifies every attestation against the
repository, release workflow, tag ref, and source commit before it publishes the
GitHub Release. Bare-metal deployment applies the same constraints to the
selected package before installation.

## Considered alternatives

Checksums alone were rejected because they do not provide a separately signed
repository and workflow identity. A long-lived local signing key was rejected
because its distribution, rotation, and protection would add an operator-owned
secret without improving the tag workflow's source binding. A floating action
tag was rejected because build dependencies must be immutable in the released
tree.

## Consequences

Operators can verify that a downloaded package digest was attested by this
repository's GitHub Actions workflow before installing it. The attestation does
not prove that the program is safe, that the build is reproducible, or that the
source review was correct. Release publication now depends on GitHub's OIDC,
attestation, and Sigstore services in addition to the existing Actions and
Release services.
