# 0029. Deliver document expansion via inbound anchor text; reject model-based doc2query

Date: 2026-07-06

## Status

Accepted

## Context

SEARCH-15 calls for **index-time document expansion**: enriching a document's
indexed representation with terms it does not literally contain but that people
use to search for it, so recall improves while the wire format and query-time
path are untouched. The canonical reference is doc2query / docTTTTTquery
(arXiv:1904.08375), which trains a sequence-to-sequence model (T5) to generate
the queries a document would answer and appends those predicted terms to the
document before indexing. The expansion terms become ordinary postings, so on a
YaCy-style DHT they ride through unchanged and ranking stays BM25 — a genuinely
wire-safe upgrade.

Two facts constrain the choice:

- **The model-free half of document expansion is already shipped.** The document
  model carries `Inlinks []AnchorText{URL, Text}`: the anchor text of every
  inbound link the crawl has seen. Ingest aggregates it per target and the search
  index maps it to a first-class `anchors` field with its own ranking weight
  (default 2, above body). Inbound anchor text is the original, model-free
  document-expansion technique — it describes a page in the vocabulary others use
  to link to it, which is exactly the query-term gap doc2query tries to close, and
  for web corpora it is famously high-value (it is how early web search worked).
  So the wire-safe expansion primitive SEARCH-15 asks for already exists and is
  retrievable and ranked.
- **The model-based half requires a heavy dependency.** docTTTTTquery needs a
  trained T5-class seq2seq model plus an inference runtime to generate predicted
  queries at index time. That is a mandatory ML model and a transformer-inference
  dependency (ONNX Runtime needs cgo; pure-Go transformer inference is not
  production-grade), which conflicts with the node's lean, cgo-free,
  no-mandatory-model posture and, per the project's dependency rule, needs its own
  ADR before adoption. The IR literature since 2019 also shows doc2query's gains
  are largely captured by, and often dominated by, hybrid dense+lexical retrieval
  (see the SEARCH-17 dense-side ADR); on a link-rich web corpus, anchor text plus
  BM25 plus per-language stemming already occupies much of doc2query's headroom.

## Decision

1. **Treat inbound anchor text as SEARCH-15's shipped, wire-safe document
   expansion.** No new work is required for the model-free path: anchor text is
   captured at crawl time, aggregated per target on ingest, indexed as the
   `anchors` field, weighted in ranking, and flows through the RWI/DHT as ordinary
   word postings. This satisfies the acceptance intent — documents are retrievable
   by expansion terms they do not literally contain, with the wire format
   unchanged.

2. **Reject model-based doc2query generation for the current architecture.** It
   requires transformer inference, model storage, dependency review, expanded
   postings, and a representative multilingual evaluation corpus. The accepted
   anchor, morphology, RM3, and positional paths already cover the demonstrated
   vocabulary failures inside the node's bounded pure-Go budget. There is no
   measured residual gain that justifies another model and poisoning surface.

## Consequences

- The strongest wire-safe expansion for web content — anchor text — is already in
  production and needs no protocol change; SEARCH-15's core value is delivered.
- No ML model, transformer runtime, or cgo dependency is added now; the node stays
  pure-Go and model-free by default.
- Documents with no inbound links still rely on their own text, morphology,
  bounded RM3, and other stored evidence. Generative expansion is not a deferred
  requirement for those pages.

## Alternatives considered

- **Adopt docTTTTTquery now.** Rejected: it makes an ML model and a transformer
  runtime mandatory at index time, breaking the cgo-free/no-mandatory-model
  posture, for a gain the anchor-text, morphology, RM3, and positional stack
  already overlap without a demonstrated evaluation win.
- **A purely lexical "pseudo-doc2query"** (mine expansion terms from the
  document's own top-TF/IDF terms). Rejected: re-injecting a document's own
  frequent terms is re-weighting, not expansion — it adds no vocabulary the
  document lacks and risks amplifying keyword-stuffed pages, which BM25 length
  normalization deliberately suppresses.
- **Query-log-derived expansion** (expand a document with the queries that led to
  clicks on it). Rejected: the node deliberately does not log query text
  (privacy), so this signal does not exist and will not be created.
