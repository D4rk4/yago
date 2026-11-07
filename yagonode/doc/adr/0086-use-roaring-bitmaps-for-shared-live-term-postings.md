# 0086. Use roaring bitmaps for shared live term postings

Date: 2026-11-07

## Status

Accepted

## Context

A long local search opens the same field and term many times within one shard
search. Each open made the search library look up the term again in every index
segment and, in a segment with deleted documents, subtract the deleted documents
from the term's documents again. Production profiling under long-query load put
term-reader opening at about 12% of node CPU, and the repeated deleted-document
subtraction at about 4%.

The index stores a term's documents and a segment's deleted documents as roaring
bitmaps. The node already obtains `github.com/RoaringBitmap/roaring/v2`
transitively through Bleve, but subtracting deleted documents once and reusing
the result requires the node to name that type and call it directly.

## Decision

Use `github.com/RoaringBitmap/roaring/v2` version `v2.14.5`, licensed under
Apache-2.0, as a direct dependency of `yagonode`. Within one shard search, the
node subtracts a segment's deleted documents from a term's documents once and
gives every reader of that term the resulting live documents.

The version stays the one Bleve already selects, so the build gains no new
module.

## Considered alternatives

Keep every open in the search library. This leaves the repeated lookups and
subtractions in place.

Pass the deleted documents to the library's per-open lookup. This is what the
library already does on every open, so it removes nothing.

## Consequences

Repeated opens of one term in one shard search cost one lookup per segment and
one subtraction per segment with deletions. Result documents and scores do not
change.

A Bleve upgrade that changes the roaring version requires a matching update
here.
