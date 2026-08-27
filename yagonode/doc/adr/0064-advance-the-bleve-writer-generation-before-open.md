# 0064. Advance the Bleve writer generation before open

Date: 2026-08-27

## Status

Accepted

## Context

ADR-0062 selected zapx v17.1.4 to correct empty-middle-chunk offsets and marked
its output as writer generation v1. Bleve issue 2393 remained open while the
maintainer prepared a release carrying the corrected zap tag. Bleve v2.6.1 now
selects zapx v17.2.3 and is the version the maintainer requested reporters to
verify.

A disposable 12,000-document index established the persistence boundary before
the production index was touched. Bleve v2.6.1 opened the v2.6.0/zapx v17.1.4
index and returned identical document and posting totals. After v2.6.1 updated,
deleted, and merged documents, v2.6.1 reopened the index with the expected
totals, while v2.6.0/zapx v17.1.4 panicked while loading a newly written field.
The old writer generation is safe input for the new reader, but new output is
not safe input for the old reader.

[WITCHER](https://arxiv.org/abs/2012.06086) and
[bounded black-box crash testing](https://arxiv.org/abs/1810.02904) support
comparing logical output over generated before-and-after states rather than
treating a successful open as a compatibility proof. The synthetic gate follows
that approach and keeps all candidate writes outside the production data
directory.

## Decision

Pin Bleve v2.6.1, zapx v17.2.3, and the exact companion API versions selected by
that release.

Treat only the exact ADR-0062 v1 generation as a compatible predecessor. Before
Scorch opens that index, persist the v2 generation marker and continue without
a document rebuild. A failed marker transition refuses startup. An already-v2
index remains byte-for-byte unchanged. Missing, malformed, or otherwise unknown
generation evidence retains the existing rebuild-from-documents path, and
startup still refuses when no authoritative rebuild source exists.

The generation transition happens before the new writer can create a segment.
This makes rollback behavior explicit: v0.0.38 sees v2 as unknown and rebuilds
the derived index before opening it. A version older than v0.0.36 has no
generation admission and must never open an index written by v2.6.1; it requires
a pre-upgrade stopped backup or a fresh derived-index rebuild.

## Consequences

Upgrading a healthy v0.0.38 index advances one 0600 marker and avoids a complete
full-text rebuild. The document vault, RWI data, crawler frontier, index mapping,
and public wire surfaces do not change.

Rollback to v0.0.38 incurs a full derived-index rebuild. Rollback to v0.0.35 or
earlier requires the operator to restore a compatible stopped backup or move
the Bleve directory and both sibling state files out of the active data root so
the older node rebuilds from documents. Copying or relabelling the v2 marker is
not a compatibility operation.

The synthetic result proves the exercised logical and writer-transition paths;
it does not prove arbitrary-corruption safety or add a decoder allocation bound.
Those remain upstream concerns in
[blevesearch/bleve#2393](https://github.com/blevesearch/bleve/issues/2393).
