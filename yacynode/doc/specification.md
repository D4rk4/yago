# YaCy-Compatible Go Peer — Technical Specification

## Context

The project implements YaCy-compatible node behavior in Go in small
compatibility-preserving slices. The target is a practical self-hosted
YaCy-compatible search peer that can join the YaCy peer-to-peer network,
exchange RWI and URL metadata, crawl configured sites, serve local and federated
full-text search, expose a Tavily-compatible Search API, and provide
operational and administrative surfaces.

The original lightweight RWI node remains the baseline implementation, not the
final product boundary. Compatibility means preserving YaCy wire protocol
shapes and observable peer behavior where Go code implements the same feature.
Go internals do not need to copy Java source code, Java storage engines, Solr,
Lucene, or Kelondro.

RWI is a compatibility and exchange layer, not the primary local search engine.
The local search target is a document store plus a full-text backend abstraction
with a modern production backend and a pure-Go fallback.

## Target Architecture

```text
yago-node
  - YaCy /yacy/* compatibility
  - peer roster, seedlists, liveness, DHT inbound/outbound
  - RWI vault + URL metadata vault
  - P2P policy, quotas, metrics

yago-searchd
  - local full-text index
  - default recommended backend: Tantivy sidecar
  - fallback pure-Go backend: Bleve or Bluge
  - document store
  - snippets, phrase/proximity, filters, facets
  - Tavily-compatible POST /search
  - YaCy-compatible /yacysearch.json and /yacysearch.rss adapter

yago-crawld
  - persistent crawl frontier
  - HTTP fast fetch path
  - optional browser slow fetch path
  - robots.txt, sitemap, canonicalization, politeness
  - content extraction and deduplication
  - emits DocumentIngest + RWI postings + URL metadata

yago-admin-ui
  - React/Next.js or Vite React
  - IBM Carbon UI framework
  - admin functionality comparable to original YaCy categories
```

## Non-Goals

* Copying Java YaCy source code into this repository.
* Requiring Java, Solr, Lucene, Elasticsearch, Kelondro, or Java runtime
  services for core Go peer operation.
* Treating RWI as the only or primary local full-text search index.
* Turning the Tavily-compatible API into a mandatory upstream Tavily proxy.
* Copying servlet-style YaCy UI internals into the admin UI.
* Claiming unsupported YaCy endpoints as compatible; incomplete surfaces must be
  explicit in documentation and status output.
* Executing remote crawl work without an explicit safety policy.

## Functional Requirements

* The node SHALL advertise one YaCy Senior peer identity.
* The node SHALL require operators to configure the YaCy peer hash and peer name it advertises.
* The node SHALL allow operators to configure the public host and port advertised in its YaCy seed.
* The node SHALL allow operators to configure the public endpoint used for YaCy-compatible reachability self-tests.
* The node SHALL announce itself through configured YaCy seedlists.
* The node SHALL serve YaCy seedlists with upstream-compatible request filters,
  including minimum peer version filtering.
* The node SHALL parse YaCy seed wire forms from configured seedlists without discarding otherwise valid peers over documented or observed `UTC` field variants.
* The node SHALL allow operators to configure a proxy for outbound connections.
* The node SHALL be reachable through one stable public endpoint.
* The node SHALL support peer discovery and peer liveness exchange.
* The node SHALL reject peer-liveness callers that present this node's peer hash or advertised endpoint as their own identity.
* The node SHALL announce in peer-liveness responses only its own seed and peers obtained from
  configured seedlists, and SHALL NOT redistribute peers self-reported in inbound requests.
* The node SHALL honor the requested peer count in peer-liveness requests and select the announced
  peers at random.
* The node SHALL receive inbound DHT RWI postings.
* The node SHALL receive URL metadata associated with RWI postings.
* The node SHALL preserve YaCy network-unit authentication behavior for inbound DHT transfer endpoints.
* The node SHALL distribute stored RWI postings and URL metadata to compatible peers when configured.
* The node SHALL verify its DHT reachability through a YaCy-compatible RWI capacity self-test before outbound DHT distribution.
* The node SHALL choose outbound DHT transfer targets using YaCy DHT ring ordering and advertised remote-index capability.
* The node SHALL recover outbound RWI postings selected for DHT handoff after restart when they have not been confirmed as accepted by a compatible peer.
* The node SHALL clear remote-index target eligibility when a DHT handoff is rejected by a peer identity currently known at the same advertised address.
* The node SHALL serve remote RWI search requests.
* The node SHALL serve local search requests through YaCy-compatible search surfaces.
* The node SHALL expose YaCy-compatible public search JSON, RSS, HTML, OpenSearch description, and suggestion subsets backed by local full-text search and DHT-selected reachable-peer search where applicable.
* The node SHALL support federated search across local and DHT-selected reachable peer results, using YaCy index abstracts for multi-term remote result conjunctions, filtering remote targets by advertised RWI inventory, and balancing redundant DHT candidates randomly.
* The node SHALL expose a Tavily-compatible `POST /search` endpoint backed by local search first, optional local semantic/vector search second, YaCy/yago peers third, and optional upstream Tavily only when explicitly configured.
* The node SHALL answer YaCy-compatible RWI capacity and status queries, including per-word RWI URL counts and zero-valued wanted-object probes.
* The node SHALL export YaCy-compatible shared blacklist files named in its configured data directory's YaCy settings after peer network-unit authentication.
* The node SHALL export YaCy-compatible peer profile properties from its configured data directory when a profile file exists.
* The node SHALL export a YaCy-compatible bounded host-link index from stored URL metadata referrer relationships.
* The node SHALL run configured crawl jobs and ingest crawler-produced documents, metadata, and postings.
* The node SHALL reject remote crawl work unless a configured policy explicitly allows it.
* The node SHALL return YaCy-compatible empty remote-crawl responses while remote crawl work is disabled.
* The node SHALL return YaCy-compatible crawl receipt retry delays while remote crawl work is disabled.
* The node SHALL store accepted RWI postings and the URL metadata those postings reference.
* The node SHALL store canonical URL, normalized URL, title, headings, extracted text, language, content type, fetch status, fetch timestamps, content hash, outlinks, and available inlink or anchor metadata for locally indexed documents.
* The node SHALL support a full-text search backend abstraction with indexing, deletion, search, and stats operations.
* The node SHOULD support Tantivy as the recommended production full-text backend through a bounded sidecar boundary.
* The node SHOULD support Bleve or Bluge as a pure-Go full-text fallback.
* The node SHALL generate snippets from the document store where document text is available.
* The node SHOULD support phrase/proximity search when the selected search backend provides positional indexes.
* The node SHALL expose machine-readable compatibility status for implemented and missing YaCy surfaces.
* The node SHALL allow operators to configure its storage quota.
* The node SHALL expose operator controls for network, crawl, index, search, and security settings.
* The node SHALL expose stable typed admin APIs for the administration UI.
* The node SHALL expose local search index availability, backend, document count,
  and update time through a stable typed admin API.
* The admin UI SHALL use IBM Carbon and SHALL be comparable by category to original YaCy administration without copying the legacy servlet UI.
* Native `yago-v2` P2P, if added, SHALL be optional and SHALL NOT change legacy `/yacy/*` compatibility behavior.

## Non-Functional Requirements

* The node SHALL durably retain accepted records before acknowledging them.
* The node SHALL apply backpressure when it cannot durably retain further accepted records.
* The node SHALL keep memory usage bounded independently of total stored RWI size.
* The node SHALL keep memory usage bounded independently of total document store
  and full-text index size.
* The node SHALL set explicit limits on all caches, queues, buffers, batches, and request bodies.
* The node SHALL complete requests within bounded deadlines.
* The node SHALL prefer availability and data integrity over ingestion throughput.
* The node SHALL support low-resource Linux-class devices, including Raspberry-Pi-class hardware.
* The node SHALL preserve compatibility with standard YaCy peer-to-peer contracts.
* The node SHALL NOT require rebuilding the complete index in memory after restart.
* The node SHALL NOT corrupt persistent state when disk is exhausted.
* Storage engines SHALL be replaceable behind a narrow interface.
* Search backends SHALL be replaceable behind a narrow interface.
* Document storage SHALL enforce size limits, retention policy, and security policy before raw content or raw-content references are stored.
* Operational behavior SHALL be observable through machine-readable metrics.
* Security-sensitive behavior SHALL default closed until configured by an operator.
* The crawler SHALL deny private, loopback, link-local, multicast, unspecified, and metadata destinations by default.
* The crawler SHALL protect against DNS rebinding by validating destinations at admission and fetch time.
