# 0030. Approve an optional, default-off CPU dense retrieval side, gated on the eval harness

Date: 2026-07-06

## Status

Accepted

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

**Conditional go.** Approve the architecture and its dependencies, ship it dark,
and gate enablement on measured quality:

1. **Architecture.** An optional, node-local dense side: a Model2Vec-static
   embedder (cgo-free, matrix lookup + mean pool) over a `coder/hnsw` graph, fused
   with the BM25 results through the existing RRF merge. The index is strictly
   local — no RWI/DHT change. The embedder sits behind a Go interface so a
   cgo-free bi-encoder (GoMLX SimpleGo path) can replace the static model later
   without touching the fusion or index wiring.
2. **Approved dependencies** (this ADR is their gate): the Model2Vec artifact
   (MIT) consumed as an exported vector matrix + tokenizer, and `coder/hnsw` (or
   an equally-maintained pure-Go HNSW), both cgo-free.
3. **Default off.** The dense side is disabled by default and controlled by an
   environment variable with a matching runtime admin setting (settings-parity
   rule). It costs no RAM and runs no code until an operator enables it.
4. **Enablement gate.** Before the dense side is recommended for general use, it
   must show an NDCG@10 improvement over the BM25 baseline on the SEARCH-16 eval
   harness (a BEIR-style subset). If the static embedder does not clear the bar,
   the interface lets us swap in a cgo-free bi-encoder and re-measure before
   shipping — we never enable a dense side that does not beat BM25 on the harness.

## Consequences

- Semantic recall becomes available as an optional, RRF-fused complement to BM25
  at near-zero query latency, with no protocol change and no cgo.
- The node stays pure-Go and model-free by default; the model artifact and ANM
  graph exist only when an operator turns the feature on.
- Memory is the binding cost when enabled (~1.3 KB/doc resident); the default-off
  posture and the per-node opt-in keep modest hardware unaffected unless chosen.
- The eval-harness gate protects the ranking baseline: a static embedder that
  would drag exact-match web queries below BM25 cannot ship enabled.
- Implementation is a separate slice; this ADR only authorizes the approach and
  the dependencies.

## Alternatives considered

- **A real bi-encoder (E5/BGE-small) as the default embedder.** Higher quality,
  but a transformer forward pass at index and query time; cgo-free Go runtimes are
  5–8× slower and the index-time CPU tax is heavy on modest hardware. Kept as the
  behind-the-interface upgrade path, not the default.
- **No dense side at all.** Rejected as a permanent stance: BM25 + anchor-text +
  stemming structurally miss paraphrase/synonym recall that dense fusion recovers
  cheaply; but this is why the dense side is *optional and gated*, not mandatory.
- **A dense side on the shared index / DHT.** Rejected outright: embeddings and ANN
  do not fit the word-hash RWI wire contract (the same boundary ADR-0031 draws for
  learned-sparse). Dense stays a strictly local, secondary signal.
