# 45. Pin container bases by digest and label source provenance

Date: 2026-07-11

## Status

Accepted

## Context

Container tags are mutable. Rebuilding an unchanged Dockerfile from a tag alone can select different
builder or runtime contents, and separately built node and crawler images do not otherwise expose the
source revision that should connect them. Operators need deterministic base selection and inspectable
cross-image provenance without adding a runtime service or network dependency.

## Decision

Every `FROM` reference in the node and crawler Dockerfiles keeps a readable release tag and pins its
resolved image by SHA-256 digest. Updating Go, Distroless, Alpine, or another base is an explicit
reviewed source change.

Both final product images carry the OCI labels `org.opencontainers.image.source` and
`org.opencontainers.image.revision`. The source label names the project repository. The revision
comes from the explicit `SOURCE_REVISION` build argument, which Compose and the e2e image targets pass
to both builds. Callers building a known checkout set `SOURCE_REVISION=$(git rev-parse HEAD)`; an
omitted stamp remains visibly `unknown`. The build does not infer a revision from a potentially dirty
worktree. The separate `VERSION` argument remains responsible for the version reported by the
binaries.

## Considered alternatives

Tag-only base references were rejected because they do not identify immutable input content.
Resolving Git state inside the Dockerfile was rejected because build contexts need not contain Git
metadata and a dirty checkout cannot be represented truthfully by a commit alone. Embedding
provenance through an external runtime registry or API was rejected because labels are available
locally through the container image configuration.

## Consequences

Base-image refreshes require deliberate digest updates. Images built with an explicit source revision
can be inspected locally and matched across the node and crawler, while unstamped development images
clearly report `unknown`. OCI labels provide traceability, not artifact signing or an assertion that
the build context was clean; security scans and release gates remain separate controls. No runtime
dependency, external API, sidecar, or additional product binary is introduced.
