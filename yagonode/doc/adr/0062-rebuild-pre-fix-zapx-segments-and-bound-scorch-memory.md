# 0062. Rebuild pre-fix zapx segments and bound Scorch merge memory

Date: 2026-08-16

## Status

Accepted

## Context

A production v0.0.35 node failed in a Scorch merge after a CRC-valid zap segment
decoded the frequency 3,172,724,355 and attempted a 406,108,717,440-byte
location allocation. The damaged term had ordinary stored documents and later
frequencies even larger than the document contents could produce.

Bleve v2.6.0 pins zapx v17.1.2. Its chunk writer converted chunk lengths to
cumulative offsets twice while an empty middle chunk retained the first-pass
offset. A minimal `[1, 0, 1]` chunk sequence therefore became `[1, 2, 3]` over a
two-byte payload. zapx v17.1.4 is the first release carrying the upstream
empty-chunk correction and returns `[1, 1, 2]`.

The document store remains the authoritative source for the derived full-text
index. The target runtime must also operate within 4 GiB; the legacy Scorch
persister default of zero allowed one worker to merge every pending in-memory
segment in one shot. An independent upstream decoder allocation bound remains
open in blevesearch/bleve issue 2393.

## Decision

Pin `github.com/blevesearch/zapx/v17` to v17.1.4 in every node build graph.

Record the corrected writer generation beside the index. Before Scorch opens an
existing index, require that exact generation. An absent or stale generation
persists the ordinary rebuild marker and rebuilds from stored documents. Startup
refuses the old index when stored documents are unavailable. A successful
rebuild writes the generation file with mode 0600 before clearing the rebuild
marker; an interruption therefore repeats the rebuild instead of serving a
partial or pre-fix index. Treat an already-present marker as a completed durable
requirement without reopening or rewriting it, including when an operator staged
the marker under a different owner before starting the service.

Configure each of the eight Scorch shards with one persister worker, a 32 MiB
maximum in-memory merge input per worker, and a 100,000-document merged-segment
ceiling. Run every current-source node in both container end-to-end suites under
an inspected 4 GiB hard memory limit with container swap disabled. Retain a
forced-merge regression whose posting list has a nonempty range, an empty middle
range, and a second nonempty range.

## Consequences

The first start after upgrading an existing index from v0.0.35 or earlier
performs one complete document-store rebuild before listeners open. Its downtime,
I/O, and temporary disk demand scale with the stored corpus; the existing
storage-headroom admission remains authoritative.

An intentional rollback to v0.0.35 or earlier must invalidate the generation
file while both services are stopped. The older writer does not update that
evidence, so retaining it across an older writer's mutations would make a later
upgrade trust a mixed-generation index. With the file absent, the later upgrade
performs another complete rebuild.

Known v17.1.2 output is rejected before its posting decoder can allocate from a
corrupt frequency. New output uses the corrected writer, and normal flush memory
is bounded independently of accumulated pending segments. The application does
not claim that a generation stamp proves arbitrary storage integrity; a future
upstream decoder bound can add defense for corruption outside this known writer
class.
