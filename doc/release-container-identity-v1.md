# Release container identity v1

This document defines the project-specific predicate used to bind a published
container manifest to an existing immutable GitHub Release. The predicate type
is the immutable commit permalink of this file.

The predicate is one JSON object with these mandatory fields:

- `schemaVersion`: integer `1`.
- `release`: GitHub Release ID, semantic-version tag, tag ref, annotated tag
  object, and peeled source commit.
- `workflow`: event, current source ref and commit, workflow ref, and workflow
  definition commit.
- `validation`: completed run ID, attempt, workflow definition commit, and the
  exact image artifact IDs, names, and SHA-256 API digests.
- `manifests`: final node and crawler manifest-list SHA-256 digests.

Verification compares the complete predicate object, its predicate-type URI,
the in-toto subject name and digest, and the GitHub-hosted signing certificate.
It rejects omitted, additional, substituted, or reordered artifact evidence.
