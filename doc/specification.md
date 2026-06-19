# YaCy-Compatible RWI Node — Technical Specification

## Context

YaCy DHT RWI participation assumes globally reachable senior peers running the full Java
implementation, which is heavy for low-resource hardware.
The project provides a lightweight Go implementation of a senior YaCy node focused on DHT RWI
storage and serving, suitable for low-resource Linux-class devices.

## Non-Goals

* Web crawling or remote crawl execution.
* HTML parsing, proxying, or content fetching.
* Full-text indexing or local search UI.
* Solr, Lucene, Elasticsearch, or equivalent full-text integration.
* Citation graphs, webgraph indexing, or media indexing.
* Outbound DHT distribution of stored RWI postings to other peers.
* A full-scale Go YaCy node beyond DHT RWI storage and serving.

## Functional Requirements

* The node SHALL advertise one YaCy Senior peer identity.
* The node SHALL require operators to configure the YaCy peer hash and peer name it advertises.
* The node SHALL allow operators to configure the public host and port advertised in its YaCy seed.
* The node SHALL announce itself through configured YaCy seedlists.
* The node SHALL allow operators to configure a proxy for outbound connections.
* The node SHALL be reachable through one stable public endpoint.
* The node SHALL support peer discovery and peer liveness exchange.
* The node SHALL announce in peer-liveness responses only its own seed and peers obtained from
  configured seedlists, and SHALL NOT redistribute peers self-reported in inbound requests.
* The node SHALL honor the requested peer count in peer-liveness requests and select the announced
  peers at random.
* The node SHALL receive inbound DHT RWI postings.
* The node SHALL receive URL metadata associated with RWI postings.
* The node SHALL serve remote RWI search requests.
* The node SHALL answer RWI capacity and status queries.
* The node SHALL reject remote crawl work.
* The node SHALL store accepted RWI postings and the URL metadata those postings reference.
* The node SHALL allow operators to configure its storage quota.
* The node SHALL NOT store full document bodies.

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
