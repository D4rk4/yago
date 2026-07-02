# YaCy-Compatible Go Peer — Technical Specification

## Context

The project ports YaCy node behavior to Go in small compatibility-preserving
slices. The target is a practical self-hosted YaCy-compatible peer that can join
the YaCy peer-to-peer network, exchange RWI and URL metadata, crawl configured
sites, serve local and federated search, and expose operational and
administrative surfaces.

The original lightweight RWI node remains the baseline implementation, not the
final product boundary. Compatibility means preserving YaCy wire protocol
shapes and observable peer behavior where Go code implements the same feature.
Go internals do not need to copy Java source code or Java storage engines.

## Non-Goals

* Copying Java YaCy source code into this repository.
* Requiring Solr, Lucene, Elasticsearch, or Java runtime services for core Go
  peer operation.
* Claiming unsupported YaCy endpoints as compatible; incomplete surfaces must be
  explicit in documentation and status output.
* Executing remote crawl work without an explicit safety policy.

## Functional Requirements

* The node SHALL advertise one YaCy Senior peer identity.
* The node SHALL require operators to configure the YaCy peer hash and peer name it advertises.
* The node SHALL allow operators to configure the public host and port advertised in its YaCy seed.
* The node SHALL allow operators to configure the public endpoint used for YaCy-compatible reachability self-tests.
* The node SHALL announce itself through configured YaCy seedlists.
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
* The node SHALL distribute stored RWI postings and URL metadata to compatible peers when configured.
* The node SHALL verify its DHT reachability through a YaCy-compatible RWI capacity self-test before outbound DHT distribution.
* The node SHALL choose outbound DHT transfer targets using YaCy DHT ring ordering and advertised remote-index capability.
* The node SHALL recover outbound RWI postings selected for DHT handoff after restart when they have not been confirmed as accepted by a compatible peer.
* The node SHALL clear remote-index target eligibility when a DHT handoff is rejected by a peer identity currently known at the same advertised address.
* The node SHALL serve remote RWI search requests.
* The node SHALL serve local search requests through YaCy-compatible search surfaces.
* The node SHALL expose YaCy-compatible public search JSON, RSS, HTML, OpenSearch description, and suggestion subsets backed by local search and DHT-selected reachable-peer search where applicable.
* The node SHALL support federated search across local and DHT-selected reachable peer results, using YaCy index abstracts for multi-term remote result conjunctions, filtering remote targets by advertised RWI inventory, and balancing redundant DHT candidates randomly.
* The node SHALL answer RWI capacity and status queries.
* The node SHALL run configured crawl jobs and ingest crawler-produced metadata and postings.
* The node SHALL reject remote crawl work unless a configured policy explicitly allows it.
* The node SHALL return YaCy-compatible empty remote-crawl responses while remote crawl work is disabled.
* The node SHALL store accepted RWI postings and the URL metadata those postings reference.
* The node SHALL expose machine-readable compatibility status for implemented and missing YaCy surfaces.
* The node SHALL allow operators to configure its storage quota.
* The node SHALL expose operator controls for network, crawl, index, search, and security settings.

## Non-Functional Requirements

* The node SHALL durably retain accepted records before acknowledging them.
* The node SHALL apply backpressure when it cannot durably retain further accepted records.
* The node SHALL keep memory usage bounded independently of total stored RWI size.
* The node SHALL set explicit limits on all caches, queues, buffers, batches, and request bodies.
* The node SHALL complete requests within bounded deadlines.
* The node SHALL prefer availability and data integrity over ingestion throughput.
* The node SHALL support low-resource Linux-class devices, including Raspberry-Pi-class hardware.
* The node SHALL preserve compatibility with standard YaCy peer-to-peer contracts.
* The node SHALL NOT require rebuilding the complete index in memory after restart.
* The node SHALL NOT corrupt persistent state when disk is exhausted.
* Storage engines SHALL be replaceable behind a narrow interface.
* Operational behavior SHALL be observable through machine-readable metrics.
* Security-sensitive behavior SHALL default closed until configured by an operator.
