# 51. Promote validated release images to attested GHCR manifest lists

Date: 2026-07-16

## Status

Accepted

## Context

The native release matrix builds, smoke-tests, and scans the node and crawler
images independently on Linux amd64 and arm64. Rebuilding those images in a
later publication job would create a different image identity that did not pass
the native checks, while attaching the two platform images separately would leave
operators without one portable release reference. A moving tag such as
`latest` would also let a historical backfill change what an existing deployment
selects.

Packages published with the repository `GITHUB_TOKEN` and linked by their OCI
source label are expected to inherit the public repository's visibility.
Container attestations identify the published manifest digest, release source,
and workflow that published it; they do not make the image safe or make the
workflow definition part of an older tagged tree.

## Decision

The native amd64 and arm64 jobs export checksum-protected archives of the images
that passed version, architecture, OCI-label, browser, and Trivy checks. A
separate publication job reloads those archives, verifies their configuration,
root filesystem, architecture, and release identity again, pushes the platform
manifests without rebuilding, and creates one Docker Schema 2
multi-architecture manifest list per product.
The public packages are:

- `ghcr.io/d4rk4/yago-node:vX.Y.Z`;
- `ghcr.io/d4rk4/yagocrawler:vX.Y.Z`.

Only the complete semantic-version tag is an operator-facing image tag.
Architecture-suffixed immutable references may stage the two child manifests,
but the workflow does not publish `latest`, major-only, minor-only, branch, or
date aliases. Each manifest list must contain exactly Linux amd64 and arm64
manifests from the same release tag and source revision. Source and revision are
verified from each child image's configuration labels because Docker's local
exporter preserves Docker Schema 2 manifests rather than OCI root annotations.
The workflow attaches GitHub-hosted provenance to each final manifest-list
digest, verifies the attestation and child membership, and makes release
publication depend on the complete registry gate. The packages are linked to
the source repository through image configuration metadata. Anonymous tag and
digest pulls are the authoritative visibility gate; if inheritance does not
make a package public, an owner changes visibility in the package settings
before retrying the gate.
Publication is serialized per release ref. Authorization, network, server, and
ambiguous registry failures stop the job instead of being treated as a missing
tag. A retry accepts an existing architecture tag only when its image identity,
architecture, source, and revision match the validated archive; it accepts an
existing final manifest only when its amd64 and arm64 child digests match
exactly. The final gate uses a fresh empty Docker credential directory to pull
both the semantic-version tag and its recorded digest for both products.

## Historical release container backfill

Release v0.0.10 uses a temporary `workflow_dispatch` path from `main` because
the workflow stored in its historical tag cannot react to a trigger added
later. The path has no caller-supplied release selection. It accepts only
release ID 355175485, tag `v0.0.10`, ref `refs/tags/v0.0.10`, annotated tag
object `09ca7be1b1e5065155111479c9213bd0566801d8`, and source commit
`9bcc0bde61364c8248fba7f452c19f2446c72898`. It verifies the published release
record, both Git objects, and main ancestry before checking out the historical
source. Package construction and GitHub Release creation remain disabled. The
native container jobs build and scan the historical source while export,
publication, and evidence tooling come from the workflow-definition commit.

The backfill attestation certificate truthfully identifies the current
`refs/heads/main` workflow invocation and its workflow-definition commit. A
custom SLSA v1 predicate records the immutable historical tag source as a
separate resolved dependency, including the exact release identity and tag
object, and records current workflow tooling as another dependency. Verification
first constrains the signed certificate to the current workflow ref, source
commit, signer workflow, and hosted runner. It then checks the signed predicate's
subject, release fields, historical dependency, workflow dependency, builder,
and invocation. It never represents the current workflow invocation as a
historical tag event.

The backfill does not rebuild packages, recreate the GitHub Release, move the
tag, replace an existing release manifest, or create a mutable alias. The
release memo receives a clearly dated factual correction only after public
pulls, digests, platforms, labels, versions, and attestations have been
verified; the temporary dispatch path is then removed.

## Considered alternatives

Rebuilding in one multi-platform job after the native matrix was rejected
because the published image identity would come from a second build that was
not smoke-tested or scanned. Publishing only architecture-specific tags was
rejected because an operator would have to choose platform details manually. A moving `latest` tag
was rejected because it weakens rollback and makes a historical backfill
capable of changing an unrelated deployment. Local workstation pushes were
rejected because they bypass the tag, source, native-test, and attestation
gates.

## Consequences

Operators receive one exact-version reference per product and may pin the
manifest-list digest for deployment. The release workflow now depends on GitHub
Actions, workflow artifacts, GHCR, OIDC, and GitHub's attestation service.
Publication expects repository-linked visibility inheritance but still proves
anonymous access. GitHub exposes package visibility through package settings;
the package REST and GraphQL surfaces do not provide a supported visibility
mutation for this workflow. Registry provenance binds a digest to recorded
build evidence but does not replace source review, Trivy policy, runtime
hardening, backups, or post-deployment health checks. No runtime dependency,
listener, service, environment variable, storage format, or YaCy wire behavior
changes.
