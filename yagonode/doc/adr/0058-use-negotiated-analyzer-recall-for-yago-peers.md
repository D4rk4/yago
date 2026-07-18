# 0058. Use negotiated analyzer recall for cooperating Yago peers

Date: 2026-07-16

## Status

Accepted

## Context

YaCy RWI addressing hashes an exact lowercased surface form. A multiword remote
query can ask index abstracts for locally observed siblings and for bounded
regular surfaces verified by supported Snowball-rule analyzers. It still cannot infer a
suppletive, analyzer-unconnected, or unsupported-language form that exists only
in a remote corpus. Project-owned language suffix lists would duplicate analyzers
incompletely, multiplying every possible variant would exceed the interactive
fan-out budget, and a stock YaCy peer has no field that carries the original
words beside their hashes. ADR-0059 defines the narrow Snowball-rule supplement;
it does not provide complete remote morphology.

The versioned Yago query-evidence request already carries bounded normalized
requirements bound to the exact ordinary query hashes on that request. A cooperating peer also has a local Bleve index whose
document analyzers provide the same morphology used by local search. It can use
those requirements during the existing inbound request without changing YaCy
RWI storage or adding another peer request.

## Decision

For a resource-producing request that successfully negotiates query-evidence
version 1, the serving Yago peer performs one strict analyzer-backed candidate
search over the wire-bound requirements. Every requirement remains mandatory.
For a primary request without a URL allowlist, the exact query-hash multiset
must equal the YaCy word hashes of those requirements. A secondary request is
eligible only inside its explicit URL-hash allowlist. This binds the otherwise
human-readable extension to the ordinary request before it can broaden recall.
The search is candidate-only, retains at most 32 hits, and has a 100 millisecond
deadline inside the request's existing deadline. Abstract-only requests and
requests with an exclusion hash, an unsupported protocol constraint, an opaque
property constraint, or a site hash retain exact RWI behavior because the peer
cannot translate those values safely into the document-index vocabulary.

The candidate search applies an explicit language modifier, site host, strict
content domain, and file type when present. A secondary request's URL hashes are
an allowlist. Candidate URLs are converted back to stored YaCy metadata rows;
missing metadata is skipped. Analyzer-ranked rows precede legacy RWI rows, exact
URL hashes deduplicate them, and the ordinary ten-row inbound cap remains final.
The peer then derives and returns the independently bounded analyzer evidence for
the merged rows.

An analyzer timeout, unavailable index, missing metadata directory, malformed
negotiation, or unsupported constraint leaves the original RWI response intact.
Stock YaCy ignores the namespaced requirements and continues exact RWI search.
The requesting node sends no additional peer request. When an already planned
single-word morphology pass addresses a different ordinary RWI hash, it
negotiates that exact surface so the serving peer can verify the hash. The
requester may then relabel the single returned ordinal with its original ranking
requirement through its own one-to-one plan; the peer cannot request or alter
that remap.

The in-memory Bleve fallback bounds candidate-only hit collection the same way
as the disk backend instead of requesting every matching document. Searches that
need post-filters or facets retain exhaustive collection for correct totals.

## Consequences

- A selected cooperating Yago peer can return a document whose required words
  exist only as analyzer-equivalent sibling inflections, even when the requester
  has never observed those surfaces in its own morphology vocabulary.
- Remote peer selection remains deliberately bounded. This improves recall
  inside the selected peer set; it does not claim exhaustive search over every
  known node.
- Negotiation adds no request beyond an already planned exact surface pass and
  adds no network round, dependency, setting, environment variable, listener,
  stored schema, or deployment surface.
- A 100-document all-matching in-memory benchmark completes the negotiated
  candidate search and metadata merge in about 6.1-6.6 milliseconds on the
  project EPYC test host. The hard 100 millisecond deadline preserves the remote
  request budget on larger or contended indexes.
- Corpus-observed and bounded Snowball-rule-derived RWI expansion remain useful
  for stock YaCy peers; negotiated analyzer recall is the bounded path for
  Yago-to-Yago federation.
- A stock YaCy peer remains addressable only by an exact surface hash the
  requester can produce. The rule-derived analyzer supplement covers
  common regular siblings absent from the local corpus, but cannot infer
  suppletive or other analyzer-unconnected forms through the compatible protocol.
