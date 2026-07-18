# 53. Use vellum for compact CJK lexicons

Date: 2026-07-17

## Status

Accepted

## Context

Chinese and Japanese dictionary segmentation needs bounded prefix lookup over several hundred thousand
terms. A Go map or pointer trie would duplicate strings and nodes in memory, while a sorted slice would
repeat binary-search work for every possible token prefix. The index already obtains
`github.com/blevesearch/vellum` transitively through Bleve, but direct use of its API requires the project
to own that dependency decision and pin.

## Decision

Use `github.com/blevesearch/vellum` version `v1.2.0`, licensed under Apache-2.0, as a direct dependency.
Generated dictionary terms and equal-width Chinese conversion keys are deterministic finite-state
transducers. The application loads only the Chinese or Japanese transducer first used by an indexing or
CJK query operation; constructing the default mapping does not decode either lexicon.

The recall path does not depend on a lexicon match. Every CJK query is analyzed into mandatory unigrams
and overlapping bigrams. A dictionary segment is an additional indexed term and an optional low-weight
query clause only. A shorter query therefore remains retrievable inside a longer dictionary word.

## Considered alternatives

A generated Go map and a pointer trie were rejected for resident-memory cost. A sorted string table was
rejected because segmentation performs repeated longest-prefix lookup. A minimal project-specific FST
reader was rejected because it would duplicate a mature component already pinned by Bleve. Runtime
GSE and Jiebago analyzers were rejected after local initialization measurements showed materially larger
startup or resident-memory costs.

## Consequences

The module moves from indirect to direct in `yagonode/go.mod` without changing its pinned version.
Lexicons remain immutable generated data and are loaded through a narrow read-only interface. A mapping
without the six versioned CJK document/query analyzers is stale and its disk shards rebuild before use.
Tests retain mandatory unigram/bigram recall when dictionary loading fails and when a query is contained
inside a larger segmented word.
