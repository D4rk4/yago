# yacydhtsearch — Technical Specification

## Context

YaCy DHT search requires a peer to originate a query, select candidate peers, and merge results.
This project's `yacynode` is a lightweight Go peer scoped to DHT RWI storage and serving; adding
live federated search to it would grow that scope beyond its purpose. The project instead
provides `yacydhtsearch`, a separate standalone Go service that performs live, synchronous
federated search across one configured YaCy DHT network — any configured network, not only
"freeworld" — exposing YaCy's own public search contract. It holds no local index and stores
nothing beyond a peer directory.

## Non-Goals

* A local index, posting cache, or result cache of any kind.
* Full-text indexing, crawling, or content fetching.
* Peer reputation, trust scoring, or abuse-prevention baked into core behavior.
* Serving more than one YaCy network per process.
* Receiving inbound DHT RWI postings or participating in DHT storage (that is `yacynode`'s role).
* Asynchronous or streaming search delivery.

## Functional Requirements

* The service SHALL accept search requests on YaCy's public `/yacysearch.json` contract and
  respond in the same format real YaCy nodes use.
* The service SHALL allow operators to configure the one YaCy network it searches.
* The service SHALL allow operators to configure the seedlists it uses to discover peers.
* The service SHALL allow operators to configure a proxy for outbound connections.
* The service SHALL query multiple peers of the configured network for each search request.
* The service SHALL NOT fail a query solely because some queried peers did not respond within
  the query's time budget.
* The service SHALL bound the total time spent on a query by an operator-configured budget.
* The service SHALL NOT return duplicate results for the same URL within a query's response.
* The service SHALL apply a configurable ordering strategy to merged results before returning
  them.
* The service MAY rank results beyond best-effort ordering if the operator configures an
  ordering strategy that assumes peer trust.

## Non-Functional Requirements

* The service SHALL set explicit limits on the peer directory, per-peer response size, in-flight
  peer calls, and per-query time budget.
* The service SHALL apply a per-call idle timeout distinct from the whole-query budget, so one
  unresponsive peer cannot exhaust the budget before reserves are tried.
* The service SHALL keep memory usage bounded independently of network size.
* The service SHALL NOT persist any state beyond the peer directory.
* The service SHALL preserve compatibility with standard YaCy peer-to-peer contracts.
* The service SHALL preserve compatibility with YaCy's public `/yacysearch.json` contract closely
  enough that any compliant client, including SearXNG's native YaCy engine, can use it unmodified.
* Peer selection, result ordering, and peer-directory eviction SHALL be replaceable behind narrow
  interfaces, with no trust or reputation mechanism assumed by the default implementation.
* Operational behavior SHALL be observable through machine-readable metrics, including per-query
  completeness.
* The service SHOULD track peer liveness and refresh its peer directory to reduce the likelihood
  of querying unreachable peers.

## Known Limitations

* Result quality depends on operators choosing to run purpose-built peers with fresher or wider
  indexes; the service itself has no mechanism to encourage or require this.
* Running multiple instances of this service for multiple networks on shared hardware is not
  resource-arbitrated across instances; each instance's own limits are independent.
* Every search is a synchronous, one-shot fan-out; at high query volume against a small network
  this produces bursts of simultaneous requests with no smoothing or backoff.
* An outbound proxy prevents the service from being tricked into contacting hosts outside the
  configured network, but does not prevent it from being used to flood legitimate peers inside
  that network.
