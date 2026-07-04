# yacycrawler — Technical Specification

## Context

`yacycrawler` is a standalone, optional, disposable crawling service. It accepts crawl
orders, fetches the web pages they reach, and publishes what it finds through its own
message-broker API, defined in the `yacycrawlcontract` module, for any consumer to
subscribe to. A YaCy node is the typical order source and consumer, but the service
depends on no consumer's internals.

The service is order-driven: it connects to a message broker on startup and idles
until crawl orders arrive. Several instances can share one order stream to spread a
crawl's load. It runs on-demand and is meant for a more capable host than an
always-on node.

## Non-Goals

* Storing or shipping document bodies in any form.
* Producing the crawl orders it consumes, or consuming its own published outputs.
* Participating in the YaCy DHT peer protocol.
* Ranking, indexing, or judging what it fetches.
* Authorizing broker subjects beyond the broker deployment's own trust boundary.
* Defeating anti-bot walls; a wall is a refusal signal to honor, not an obstacle to evade.
* Guaranteeing delivery beyond the broker's own semantics.

## Functional Requirements

* The service SHALL idle until a crawl order arrives, then process it.
* The service SHALL spread a crawl's load across instances that share one order stream.
* The service SHALL crawl only what an order's profile admits, from its seeds and
  discovered links.
* Every crawl run SHALL terminate, bounded by profile admission and a run-wide page budget.
* The service SHALL honor each host's stated crawl limits and treat any refusal signal,
  including anti-bot walls, as a limit to obey rather than circumvent.
* The service SHOULD fetch and publish the same content at most once per run, regardless
  of how many URLs address it.
* Every outbound fetch SHALL egress through the operator's configured proxy.
* The service SHALL offer two equal outputs, each operator-enabled on its own: an index
  output carrying page references, never a body, and a page-content output carrying content.
* The service SHALL publish each page to every enabled output, advancing them together:
  if any cannot accept, the others wait.
* Every fetched page SHALL reach one terminal outcome: published to all enabled outputs,
  or disposed per operator policy.
* A publication SHALL fail only on a hard, non-retryable broker error; transient
  backpressure waits within the run deadline.
* A publication failure SHALL NOT be terminal; the page stays unpublished and its order
  un-acked.
* The service SHALL acknowledge an order only after every page it produced reached a
  terminal outcome.

## Non-Functional Requirements

* The service SHALL process each order idempotently per its identity under at-least-once
  delivery.
* Any URL normalization the service applies SHALL preserve content identity, never
  treating distinct content as the same.
* The service SHALL keep memory usage bounded independently of a crawl run's size.
* The service SHALL set explicit limits on its frontier, queues, buffers, and
  fetched-body sizes.
* The service SHALL bound every outbound fetch and every run with explicit deadlines.
* The service SHALL persist no state of its own; a run survives a restart only when the
  order source re-sends the order.
* The message broker SHALL be replaceable behind a narrow interface, with no change to
  crawl logic.
* The page-fetch mechanism SHALL be replaceable behind a narrow interface, with no
  change to crawl or admission logic.
* Operational behavior SHALL be observable through machine-readable metrics.

## Known Limitations

* Defense against internal-host targeting and DNS rebinding depends on the configured
  proxy's egress policy; the service adds none of its own.
* The page-content output carries renderable web content; anyone with broker publish
  rights can inject it, and narrowing that subject is operator-added hardening.
* A consumer's outage-survival for the page-content output holds only within the
  retention window the operator sizes.
