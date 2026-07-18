# 0059. Use Snowball rules for bounded surface generation

Date: 2026-07-16

## Status

Accepted

## Context

Stock YaCy peers address RWI rows by the exact hash of a surface word. Corpus-observed expansion and
the negotiated Yago evidence extension cannot discover a sibling inflection that exists only on a stock
peer. Maintaining project-specific suffix lists would duplicate the local analyzers incompletely and
would make recall depend on manually curated language facts.

Bleve already brings `github.com/blevesearch/snowballstem` into the module graph and the local analyzers
use its generated stemmers. Directly reading the same generated rule tables lets the requester propose
surface candidates without adding another language model or a runtime data source. Direct use still
requires an explicit dependency decision and pin.

## Decision

Use `github.com/blevesearch/snowballstem` version `v0.9.0`, licensed under BSD-3-Clause, as a direct
dependency. The node derives candidate roots and suffixes from exported Snowball rule tables for its
Arabic, Danish, Dutch, English, Finnish, French, German, Hungarian, Italian, Norwegian, Portuguese,
Romanian, Russian, Spanish, Swedish, and Turkish analyzers. Every proposed surface is passed back
through the analyzer that produced it and retained only when it produces the query word's stem.

A short query does not identify one language reliably. Candidate generation therefore retains every
applicable rule-backed analyzer identity, including an analyzer whose stem is unchanged or equal to
another analyzer's stem, and round-robins proposals across those identities under the shared attempt
bound. Each proposal is retained only when its proposing analyzer maps it back to that analyzer's query
stem. Duplicate surfaces collect the distinct analyzer identities that verify them. One global order
then prefers more distinct-analyzer agreement, shorter edit distance and length difference, greater
retained prefix and rule-table support, higher analyzer priority, and lexical order. The original
remains first and the globally ranked output stops at the shared result bound. Analyzers without an
exported matching Snowball rule source continue to contribute corpus-observed forms only.

Generation is bounded to 2,048 candidate attempts and 12 returned surfaces before the existing
per-query morphology planner applies its stricter network limits. The original surface remains first.
Rule-based generation accepts terms from four through 32 Unicode runes; shorter or longer terms
return only their normalized base form. No generated table is copied into the repository, and no
network request, external process, setting, or deployment-specific resource is added.

## Considered alternatives

Hand-written suffix maps and localized synonym lists were rejected because they duplicate semantic
language knowledge and drift from the actual index analyzers. Copying generated Snowball tables was
rejected because it would fork an already pinned dependency. Corpus-only expansion was retained but is
insufficient for a surface absent from the local corpus. Exhaustive dictionaries and model-based query
generation were rejected because their memory, latency, and operational cost do not fit the interactive
swarm budget.

## Consequences

The module moves from indirect to direct in `yagonode/go.mod` without changing its pinned version.
Common regular sibling inflections covered by the supported analyzers can be addressed on stock YaCy peers even when
the requester has not indexed them. The generator cannot infer suppletive or irregular forms that the
analyzer rules do not connect, and it does not claim complete morphology for every language. Pinning
isolates the use of exported generated rule tables from upstream table-layout changes until an
intentional dependency upgrade.
