# 0030. Reject a CPU dense retrieval side for the current architecture

Date: 2026-07-06

## Status

Superseded by [ADR-0048](0048-bounded-google-yandex-ranking-research-disposition.md)

## Context

SEARCH-17 proposes an optional, node-local **dense** retrieval side (vector
embeddings + approximate nearest neighbour) fused with the existing BM25/scorch
lexical results through the RRF merge (SEARCH-12), kept strictly local so the DHT
wire contract is untouched. It must fit the node's hard constraints: pure Go
(`CGO_ENABLED=0`), no mandatory GPU/LLM, runnable on modest hardware. This ADR is
the dependency go/no-go the constraint "new third-party deps need an ADR first"
requires before any dense code lands.

Findings (current to 2024–2026):

- **A no-inference embedder exists and is pure-Go-feasible.** Model2Vec / "potion"
  (MinishLab, MIT) distills a sentence-transformer into a **static per-token
  vector table**: at inference there is **no transformer forward pass** — text is
  tokenized, per-token vectors are gathered from a matrix and mean-pooled (a
  gather + mean, ~500× faster than the source model on CPU). The artifact is just
  an embedding matrix (256-dim; ~30 MB for the 8M variant, ~120 MB for 32M) plus a
  tokenizer; official Rust and Dart ports confirm reimplementation is trivial, so
  a cgo-free Go embedder is a few hundred lines plus a pure-Go WordPiece
  tokenizer. Query embedding is microseconds.
- **A pure-Go ANN exists.** `github.com/coder/hnsw` is a maintained, cgo-free,
  in-memory HNSW with disk persistence — production-viable for a single-node,
  RAM-resident, 256-dim index of modest scale. There is no pure-Go faiss/hnswlib
  equivalent (no PQ, no disk-resident billion-scale, no quantization), and the
  graph must stay in RAM: ≈1.2–1.5 KB/doc resident (~1.3 GB per 1M docs).
- **Quality is the open risk.** Strong bi-encoders (bge-small ~52, e5-small ~46
  MTEB-retrieval) beat BM25 (~43) and give consistent, if modest, RRF hybrid lifts
  (+1–7% nDCG, larger out-of-domain). But **Model2Vec-static is weaker**:
  potion-retrieval-32M scores ~35 MTEB-retrieval — below BM25 standalone. As an
  RRF *complement* it can still add semantic recall on paraphrase/synonym/
  conversational queries that BM25 structurally misses, but the hybrid gain over
  BM25 alone is smaller and less certain than with a real bi-encoder, and may be
  flat or negative on the exact-match, navigational, rare-entity queries that make
  up much of web traffic. No published BEIR head-to-head of a static-embedding
  hybrid vs BM25 was found — this is the central evidence gap.
- **Cost is dominated by RAM, not latency.** Added query latency is ~1–5 ms
  (microsecond embed + sub-ms HNSW + negligible RRF); the real cost is the
  RAM-resident graph and vectors (~1.3 KB/doc), a genuine constraint on modest
  hardware. Real bi-encoders additionally pay a large index-time CPU tax (a
  transformer pass per document), and cgo-free transformer runtimes in Go (GoMLX
  SimpleGo, onnx-gomlx) are ~5–8× slower than ONNX Runtime.

## Decision

Reject the dense side for the current architecture. The earlier conditional
approval is superseded and grants no dependency approval.

1. The static embedder is weaker than the lexical baseline in the cited public
   evaluation, and Yago has no representative held-out evidence that its fusion
   improves the measured search failures.
2. The in-memory graph and vectors add roughly 1.3 KiB per document in the
   reviewed design, conflicting with the modest-hardware target and current
   storage bounds.
3. A stronger bi-encoder adds a transformer runtime and document-wide inference
   cost. No measured latency or indexing reserve justifies that runtime boundary.
4. Multilingual analyzers, anchors, RM3, ordered and unordered proximity,
   original-gap agreement, authority, freshness, and learned lexical evidence
   address the demonstrated backlog without dense request-time semantics.

## Consequences

- No dense index, model artifact, dependency, environment variable, admin
  setting, or request-time stage is added.
- RWI remains the YaCy exchange format and the local index remains the bounded
  Bleve lexical path.
- Dense retrieval is not a deferred ranking requirement. A materially different
  architecture and representative evidence would require a new ADR rather than
  reopening this approval implicitly.

## Alternatives considered

- **A real bi-encoder (E5/BGE-small).** Rejected: it adds a transformer forward
  pass at index and query time plus a model runtime without representative Yago
  evidence or a measured resource budget.
- **A static Model2Vec and HNSW side.** Rejected: its reviewed retrieval score is
  below BM25 and the resident graph is too costly for an unproven fusion gain.
- **A dense side on the shared index / DHT.** Rejected outright: embeddings and ANN
  do not fit the word-hash RWI wire contract (the same boundary ADR-0031 draws for
  learned-sparse). A future independently approved design could only be local;
  this ADR does not approve one.
