# 0032. Do not adopt Tiptoe-style private search; keep it behind the dense side

Date: 2026-07-06

## Status

Accepted

## Context

SEARCH-20 is a research spike: assess hiding the query **content** from the peers
that serve a distributed search — not just from logs — Tiptoe-style (private
nearest-neighbour over document embeddings via linearly-homomorphic encryption),
and decide with a recommendation. The deliverable is this ADR.

Today a distributed search sends the query's **word hashes** to the DHT peers
responsible for those hashes, so a serving peer learns the (hashed) query terms.
The privacy goal is to let peers answer without learning what was asked.

Findings:

- **What Tiptoe is (SOSP 2023, Henzinger, Dauterman, Corrigan-Gibbs, Zeldovich —
  MIT).** Private search built on private nearest-neighbour over **document
  embeddings**: offline, a central pipeline embeds the whole corpus
  (distilbert-tas-b, PCA-reduced to 192 dims), k-means-clusters it into ≈√N
  topic clusters, and preprocesses the crypto matrices; per query the client
  embeds locally, picks the nearest cluster from cached centroids, and sends an
  **LWE/SimplePIR-style linearly-homomorphic** encryption of the query embedding,
  against which the server computes encrypted inner products it cannot read.
  Its **privacy is cryptographic and single-server** — a server learns nothing
  even seeing the full interaction (LWE semantic security), with no non-colluding
  assumption; a malicious server can break correctness or availability but not
  privacy. The cost, however, is a server-cluster cost: on a 360M-page corpus over
  45 servers the paper reports **~56.9 MiB communication and ~145 CPU core-seconds
  per query, ~2.7 s latency, ~$0.003/query**, each server holding an 8–12 GiB
  shard, plus a one-time corpus preprocessing of ~1,300 core-hours and ~92 GPU-
  hours. Accuracy is close to classical tf-idf (MRR@100 ~7.7 vs ~2.3 for exhaustive
  neural).
- **Wally (arXiv:2406.06761, 2024)** improves throughput for **many concurrent
  clients** (decoy queries over an anonymous network, batched, BFV responses) but
  **relaxes privacy from cryptographic to (ε,δ)-differential** and needs a large
  honest-client population plus an anon-network dependency — the authors note that
  for few (hundreds of) clients "Tiptoe would be more suitable." Later work
  (Tiptoe++, Pacmann, Compass) all keep the central preprocessed dense-index model.
- **Architectural fit with our DHT/RWI design is poor, on three independent axes —
  and the mismatch is the *system model*, not the privacy guarantee:**
  1. **Dense vs lexical.** Tiptoe operates over **embeddings**; our shared index
     is a lexical RWI keyed by word hash. Private search therefore *presupposes*
     the optional dense side (SEARCH-17 / ADR-0030), which is itself default-off
     and node-local, not a swarm-wide embedding index. There is nothing to make
     private otherwise.
  2. **Central preprocessing vs DHT partitioning.** Tiptoe needs a globally
     consistent, centrally-clustered, topic-sharded index. Our index is
     partitioned by word hash across peers with no central build step and no global
     embedding space. The two sharding schemes are incompatible and there is no
     coordinator to do the preprocessing.
  3. **Stable well-resourced server tier vs volunteer churn.** Tiptoe's per-query
     linear scan (~145 core-s) over an 8–12 GiB resident shard assumes servers that
     persist and are well provisioned. Volunteer peers churn and cannot be asked to
     hold such shards or burn 145 core-s per served query. The blocker is topology
     and resources, not the cryptographic trust model (which is actually strong).
- **A candour note on today's baseline.** The current word hashes are
  **deterministic hashes of dictionary words**, so a curious peer can precompute a
  rainbow table and reverse common terms — the hashing is obfuscation, not privacy.
- **Cheaper partial-privacy measures that fit a DHT** exist and are far less costly
  than PIR: decoy/cover hashes (send k−1 plausible decoy word hashes for
  k-anonymity at the serving peer), padding, and contacting peers over Tor or a
  mixnet. These trade a bounded overhead for **probabilistic** query privacy —
  history-based attacks (SimAttack class) recover roughly half of obfuscated
  queries, so it is not the cryptographic hiding Tiptoe gives — but they are
  deployable on the existing wire.

## Decision

**No-go** on Tiptoe-style private search for now. It is not a near-term option
because it presupposes machinery this node does not have and a trust/topology model
the swarm does not provide:

- it requires a swarm-wide **dense embedding index**, which does not exist (the
  dense side is optional, default-off, and strictly local — ADR-0030);
- it requires **central preprocessing and a stable, well-resourced server tier**
  holding large topic shards, which contradict the DHT's churning,
  word-hash-partitioned, volunteer-peer design; and
- its per-query cost (~57 MiB communication, ~145 core-seconds) is a server-cluster
  cost, disproportionate for a mesh where any peer might serve any query on modest
  hardware.

The decisive prerequisite is therefore ADR-0030's dense side shipping and maturing
first; only then is there an embedding index a PIR scheme could even operate over,
and even then the central-preprocessing and trust-model mismatches remain unsolved
for a permissionless swarm.

## Consequences

- Query privacy against peers stays a **known limitation**, honestly documented,
  rather than a solved property; the node continues to protect query text in logs
  (query-log mode) but not from a serving peer.
- The pragmatic near-term direction, if query-against-peer privacy is prioritized,
  is a **lightweight partial-privacy measure** — decoy/cover word hashes for
  k-anonymity at the serving peer, and/or peer transport over Tor — tracked as its
  own task with its own cost/benefit, not this cryptographic-PIR spike.
- Revisiting Tiptoe/Wally is contingent on (a) a swarm-wide dense index existing
  and (b) a workable trust/topology model for private retrieval over churning
  peers; both are open research, so this stays parked.

## Alternatives considered

- **Implement SimplePIR/Tiptoe over the local dense index only.** Rejected as
  pointless for the stated goal: the privacy threat is the *remote* peer learning
  the query; a purely local PIR hides nothing from anyone who was not going to see
  the query anyway.
- **Decoy/cover hashes (k-anonymity) now.** A real, cheap, DHT-compatible partial
  mitigation — but it is a different, lighter design than this spike's PIR scheme,
  and it weakens the DHT's targeting/bandwidth; it is recorded here as the
  recommended near-term direction and left to a dedicated task rather than adopted
  under a "Tiptoe" heading.
- **Route peer queries over Tor/a mixnet.** Hides the network origin, not the query
  content from the serving peer; complementary to, not a substitute for, query
  hiding. Noted as a transport-level option for a future privacy task.
