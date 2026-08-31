# 0078. Use requirement-aware lexical candidates

Date: 2026-08-31

## Status

Accepted

Amends [ADR-0070](0070-bound-analyzer-scoped-search-with-complete-lexical-candidates.md)
and [ADR-0072](0072-rerank-lexical-candidates-within-one-search-snapshot.md).

## Context

An anonymous production query for `hipotekarna podgorica` could return an
incomplete zero-row page on its first read and local rows on an immediate
repeat. Nearby longer queries reproduced the same first-read failure at the
1.8-second interactive deadline. The node remained ready, neither service
restarted, and its cgroups recorded no pressure or OOM event.

An isolated DDGS provider request returned relevant rows within the configured
provider budget. On an isolated clone of the production Bleve tree, the
existing positive-term union admitted 1,786 candidates for `hipotekarna
podgorica`, while only seven documents satisfied the complete lexical
requirements before analyzer scope. Two five-term variants admitted 51,703 and
45,683 union candidates although neither had an exact requirement match.

ADR-0070 deliberately chose the union of every positive term as a conservative
superset. ADR-0072 moved that collection into each child reader and retained a
4,097th sentinel. The union is unnecessarily broad for multi-term requests: one
common word can cross a child's sentinel even when the required conjunction or
relaxed minimum is selective. That child then performs the exhaustive
analyzer-scoped query.

The authoritative query already expresses its lexical admission rule once per
analyzer branch. Removing only the analyzer-scope clause from such a branch
creates a complete superset of that branch. A strict or fuzzy branch still
requires every retained term. A relaxed branch still requires its calculated
minimum. Analyzer-specific stopword removal remains local to that branch.
Bleve's conjunction searcher orders its children and supports an unscored
conjunction optimization, so the selective requirement can lead the candidate
walk without changing final scoring. See the Bleve v2.6.1
[`ConjunctionSearcher`](https://github.com/blevesearch/bleve/blob/v2.6.1/search/searcher/search_conjunction.go).

## Decision

Build the same-reader candidate query as a disjunction of complete lexical
requirement branches rather than a disjunction of individual positive terms.
Retain one unscoped standard-analyzer branch. Retain each analyzer branch whose
whole-query token signature is not equivalent to the standard branch, plus
every independently required dictionary branch. Omit analyzer scope, excluded
terms, domain constraints, and optional expansion terms from the candidate
query because the unchanged final query remains authoritative for them.

For strict and fuzzy retrieval, each candidate branch requires every term that
its analyzer retains. For relaxed retrieval, each branch applies the same
minimum-term rule as the final branch. A branch that drops every term is absent.
Duplicate submitted terms are collapsed before branch construction.

Keep the existing per-child same-reader contract. At most 4,097 candidate
identities are inspected. A complete set of at most 4,096 becomes the
zero-score internal-identity filter. The sentinel remains MatchAll and therefore
falls back to exhaustive final retrieval without truncation. Explicit search
explanations retain the exhaustive path.

At the outer interactive deadline, consume an already queued pipeline outcome
before synthesizing a timeout response. This does not wait beyond the deadline
and does not treat unfinished work as complete. It removes only the select race
that could discard a result already delivered by the worker.

## Consequences

Multi-term candidate work follows the query's actual lexical admission shape.
The isolated production copy reduced the three observed candidate populations
from 1,786 to 7, from 51,703 to 0, and from 45,683 to 0 while preserving final
hit totals. These warm-copy observations demonstrate selectivity and result
equivalence; they are not a cold production latency or concurrency claim.

Single-term behavior and the conservative overflow path are unchanged. A broad
multi-term requirement can still overflow and use exhaustive retrieval. The
native Bleve alias remains authoritative for identity, order, BM25 score, field
scores, totals, status, errors, and cancellation.

Tests prove admission of a complete strict, relaxed, fuzzy, and
analyzer-stopword match; refusal of documents below each requirement; exact
equivalence with the exhaustive scoped query; preservation of a queued outer
completion; and refusal of unfinished work. The change adds no dependency,
stored format, setting, environment variable, listener, service, image,
package topology, contract, or wire shape.
