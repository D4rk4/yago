# 0087. Read shared term postings through the segment API

Date: 2026-11-07

## Status

Accepted

## Context

A long local search opens the same field and term many times within one shard
search. Each open made the search library open the field's term dictionary and
look up the term in every index segment again. Production profiling under
long-query load put term-reader opening at about 12% of node CPU.

Reusing one term's postings across those opens requires the node to read term
dictionaries, postings lists and postings iterators of each segment itself. The
search library exposes them through `github.com/blevesearch/scorch_segment_api/v2`,
which the node already obtains transitively through Bleve.

## Decision

Use `github.com/blevesearch/scorch_segment_api/v2` version `v2.4.10`, licensed
under Apache-2.0, as a direct dependency of `yagonode`. Within one shard search,
the node opens each field's term dictionary once per segment, reads each term's
postings once per segment, and gives every reader of that term fresh iterators
over the shared postings.

The reader reproduces the library's document order, document numbers, term
frequency, norm and term-vector output, including seeking backward. It does not
offer the library's internal conjunction shortcut, which does not apply to
scored searches.

The version stays the one Bleve already selects, so the build gains no new
module.

## Considered alternatives

Cache the library's own term readers. A term reader holds iterator state and
cannot be shared by several searchers.

Rewrite the query to open each term once. The query tree combines one term in
several clauses with different boosts and phrase positions, so a rewrite would
change scores.

## Consequences

Repeated opens of one term in one shard search no longer repeat dictionary
opening or term lookup. Result documents and scores do not change; tests compare
both readers output for output and production queries score for score.

A Bleve upgrade that changes segment reader behavior requires the comparison
tests to pass before the upgrade lands.
