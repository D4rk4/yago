# 0057. Negotiate bounded cross-node query-match evidence

Date: 2026-07-16

## Status

Accepted

## Context

YaCy metadata rows expose only bounded visible fields. A receiving node can
reanalyze those fields, but it cannot recover trustworthy document-wide body
offsets or stored-field positions. This weakens morphology-aware proximity and
highlighting for results returned by another Yago node. Stock YaCy has no
equivalent position exchange, and unsolicited offsets from a peer cannot be
trusted as ranking evidence.

Unicode Standard Annex 29 defines word boundaries as inputs to word selection
and requires language-specific tailoring for some scripts. Bleve highlighting
uses stored originals and term vectors, while Lucene's Unified Highlighter also
permits bounded reanalysis as an offset source. Yago already stores the source
document and has a bounded analyzer evidence path, so the serving peer can
reanalyze that stored document without adding term vectors or another index.

The relevant primary sources are:

- [Unicode Standard Annex 29: Unicode Text Segmentation](https://www.unicode.org/reports/tr29/)
- [Bleve highlighting](https://blevesearch.com/docs/Highlighting/)
- [Lucene UnifiedHighlighter](https://lucene.apache.org/core/10_3_1/highlighter/org/apache/lucene/search/uhighlight/UnifiedHighlighter.html)

## Decision

Yago extends `/yacy/search.html` with an optional, namespaced version-1
query-match-evidence exchange. A resource-producing request carries the version
and the normalized requirements represented by that exact wire request. The request retains at most 32
nonempty UTF-8 requirements, 256 bytes per requirement, and 4 KiB in total.
For a primary request without a URL allowlist, their YaCy word-hash multiset
must equal the ordinary query-hash multiset. A secondary request is eligible
only within its explicit URL-hash allowlist. Index-abstract-only requests do not
negotiate evidence.

A Yago peer responds only after successful version negotiation and only for
metadata rows backed by its local document directory. It reuses the stored
document analyzer and returns evidence keyed by the row's URL hash: analyzer
identity, an optional bounded snippet with relative match ranges, absolute body
match ranges, and requirement-ordinal positions for title, headings, inbound
anchors, body, and normalized URL. Each item explicitly carries the complete
analyzer-relevant ordinal set and the subset absent from every analyzed field.
The bounded position allocator reserves one witness for every present relevant
ordinal before retaining additional positions. One request analyzes at most 32
candidates, 2 MiB of stored source in total, 512 KiB per document, and 100 milliseconds. It
retains at most 128 KiB of base64 wire values across the response. One resource
retains at most 16 KiB of JSON before base64 encoding, five fields, 32 requirement entries per field,
64 positions per requirement, 256 positions in total, 128 snippet matches, 128
body matches, and a 2 KiB snippet.

The receiver binds every ordinal to the exact normalized requirement list that
its encoded request carried. A locally planned single-word morphology pass may
map that one validated ordinal back to the original ranking requirement, but
the peer supplies no remapping data and a multiword or cardinality-changing
remap is rejected. Before ranking the receiver independently validates the
version, registered analyzer identity, analyzer compatibility with the result's
language and visible script, UTF-8 boundaries, monotonic offsets and positions,
allowed field names, resource hash, and every size limit. It reproduces the
analyzer-relevant ordinal set locally and rejects missing, extra, overlapping,
or falsely absent ordinals, so sparse position evidence cannot reduce the
coverage denominator. A nonempty validated
wire snippet replaces the metadata snippet together with spans relative to that
exact text. A valid response without a replacement snippet publishes an
authoritative non-nil empty snippet-span set, preventing structural highlighting
from reinterpreting the retained metadata snippet. Missing, unsupported,
malformed, incompatible, or unmatched evidence is ignored and the existing
bounded visible-field analyzer remains available.

Stock YaCy peers ignore the namespaced request fields and receive the unchanged
legacy request and response behavior. This evidence exchange does not change RWI
addressing or multiply peer fan-out. A cooperating Yago peer may also use the
negotiated wire-bound requirements for bounded analyzer-backed retrieval; that
retrieval has independent limits and failure behavior.

## Consequences

- A cooperating Yago peer can supply bounded stored-document morphology and
  proximity evidence without Bleve term vectors or copying remote offsets into
  the local document store. Validated snippet and body spans are deep-cloned
  with the ordinary bounded search session, so later pages reuse the same spans
  instead of reconstructing them from visible fields. Stored-field positions
  are consumed by final ranking and discarded before session retention.
- When several peers return the same URL, reciprocal-rank and peer-reputation
  contributions still combine independently, while the retained row upgrades
  to the strongest authoritative analyzer payload. A lower-sorted legacy peer
  therefore cannot discard a cooperating peer's snippet, body spans, or stored-
  field positions before final ranking.
- Peer evidence is untrusted until local structural, analyzer, script, identity,
  and budget validation succeeds.
- Legacy peers and responses remain interoperable; malformed extensions degrade
  to the existing visible-field analyzer rather than suppressing evidence.
- Evidence generation adds bounded document reads and reanalysis only when both
  nodes negotiate version 1. No setting, environment variable, dependency,
  listener, stored schema, or deployment surface is added.
- The same negotiation can carry analyzer-backed recall without adding another
  request field or network round trip; evidence validation remains independent
  from how a row was retrieved.
