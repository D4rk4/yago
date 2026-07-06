# 0031. Do not move the RWI to learned-sparse (SPLADE) weights

Date: 2026-07-06

## Status

Accepted

## Context

SEARCH-19 is a research spike: assess moving the distributed Reverse Word Index
(RWI) toward **learned-sparse** representations (SPLADE-v3, arXiv:2403.06789)
served by a fast sparse ANN (Seismic, arXiv:2404.18812), and decide go/no-go with
the wire-compatibility risk made explicit. The deliverable is this ADR, not code.

The RWI is the node's shared wire contract with the YaCy swarm: peers store
postings keyed by a **word hash over normalized whitespace words** on a DHT, each
posting carrying an **integer term weight**, and peers merge each other's postings
on a shared word hash. Stock YaCy peers participate in the same DHT.

Findings (grounded in the SPLADE-v3, Seismic, and docTTTTTquery literature):

- **What SPLADE is.** SPLADE encodes text into a sparse vector over BERT's
  **WordPiece subword vocabulary (~30k tokens)** using a Transformer encoder plus
  the masked-language-model head, producing **learned float weights** and
  performing **term expansion** — it activates vocabulary tokens the document
  never contained, which is where its vocabulary-mismatch gains come from. Full
  SPLADE runs a neural forward pass at **both index and query time**; the
  "SPLADE-doc" variant drops the query encoder (unit-weight query tokens) but its
  terms are **still WordPiece tokens**. Quality is strong: SPLADE-v3 beats BM25
  significantly and is competitive with cross-encoder rerankers on MS MARCO, with
  ~+2% out-of-domain on BEIR over prior SPLADE.
- **Seismic** is a fast approximate engine for learned-sparse vectors (geometric
  block organization + summary-vector pruning; sub-millisecond single-thread on MS
  MARCO). Its implementations are **Rust (official) and Java (OpenSearch)** with
  Python bindings; there is **no Go implementation** and no cgo-free path — and it
  presupposes a learned-sparse, per-node index, so it is only relevant *after* the
  wire change SPLADE would force.

## Decision

**No-go.** The RWI will not adopt learned-sparse (SPLADE) weights as its shared
DHT representation. Three wire-compatibility problems, the first decisive:

1. **Tokenization / key-space mismatch (decisive).** The RWI key is a hash of a
   whitespace word; SPLADE's terms are **WordPiece subword tokens** from BERT's
   vocabulary — a different alphabet with a different hashing basis. Adopting
   SPLADE changes *what is hashed and stored*, so the DHT key space is no longer
   YaCy word hashes. Stock and non-upgraded peers can neither address nor
   interpret those postings. This is a semantic change to the RWI key contract,
   not a tunable parameter, and it breaks swarm interop. SPLADE-doc does not help:
   its keys are still WordPiece tokens.

2. **Term explosion vs posting backpressure.** SPLADE expands each document to
   hundreds of nonzero dimensions, inflating postings-per-document by roughly an
   order of magnitude. On a DHT that multiplies storage, replication, and
   peer-to-peer transfer volume and directly antagonizes the existing inbound
   posting-count backpressure and pruning. FLOPS-regularized variants reduce but
   do not remove this.

3. **Cross-peer weight incoherence.** In a permissionless swarm most peers will
   never upgrade, so merges mix BM25 postings (corpus-statistics, TF-IDF-scaled)
   with SPLADE postings (learned, log-saturated activations) on the same word
   hash. The two weight systems are on non-comparable scales with different
   meanings; summing and ranking them yields meaningless scores, and there is no
   shared normalization across a heterogeneous fleet. (Integer quantization of
   SPLADE impacts is a solved, lesser issue — the incoherence is the unsolvable
   one.)

## Consequences

- The RWI stays word-hash-addressed with integer weights, fully interoperable with
  stock YaCy peers; the swarm wire contract is preserved.
- The **wire-safe subset** of learned-sparse's benefit — mitigating vocabulary
  mismatch by **expansion into real whole words** — is captured instead by
  document-side expansion injected as ordinary word postings: inbound anchor text
  (already shipped and indexed) and, if it clears its gates, model-based
  doc2query (ADR-0029). Those keep YaCy word hashes and integer weights and
  degrade gracefully among non-upgraded peers.
- If a full learned-sparse engine is ever wanted, it belongs in an **optional,
  strictly local secondary index or re-ranker (Seismic-style), never on the DHT
  wire** — the same "keep it off the shared contract" boundary the dense-side ADR
  (0030) draws. Building that would additionally require a cgo-free Go sparse-ANN,
  which does not exist today.

## Alternatives considered

- **Learned-sparse only on the document side, whole-word (doc2query).** This is
  the wire-safe path and is adopted as its own decision in ADR-0029 — but it is
  *not* SPLADE: it recovers the expansion-into-real-words half, not the
  learned-subword-weights half.
- **A parallel SPLADE key space alongside the word-hash RWI.** Rejected: it forks
  the swarm into incompatible index planes, doubles storage, and still cannot be
  merged with stock peers — all the cost of the wire break with none of the
  interop.
- **cgo/FFI to the Rust Seismic crate for a local dense-sparse re-ranker.**
  Deferred to the dense-side track (ADR-0030); it violates the cgo-free build and,
  regardless, does not touch the DHT contract this spike is about.
