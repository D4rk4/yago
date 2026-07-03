# yago Roadmap

This is a plain-language summary of where `yago` is going. It is a direction, not
a claim that every capability below is complete. Endpoint-level status is in
[compatibility.md](compatibility.md); the full engineering plan is in
[PLAN.md](../../PLAN.md).

## What yago Is

A self-hostable Go search appliance that joins the YaCy peer-to-peer network,
optionally crawls the web, answers local and federated searches, exposes a
Tavily-compatible Search API, and is administered through a typed API and a web
UI.

## Direction By Area

- **Peer-to-peer network.** Stay a well-behaved YaCy peer: liveness, seed lists,
  RWI and URL metadata exchange, remote search, and outbound DHT distribution
  that matches YaCy sender behavior.
- **Search.** Move local search onto a document store with a full-text backend,
  keep RWI as the exchange and DHT-interop format, and merge local results with
  federated results from reachable peers.
- **Tavily-compatible API.** Serve a Tavily-shaped search and extract API from the
  node's own search core. An optional real upstream Tavily mode is planned but off
  by default.
- **Crawler.** Run the crawler as a separate, optional worker that fetches pages
  under strict safety and politeness rules and streams results back to the node
  for indexing.
- **Administration UI.** Provide a web UI for setup, search, crawler control,
  network status, and index and configuration management, built on the typed
  admin API.
- **Security and privacy.** Authenticated administration, scoped API keys,
  default-deny cross-origin and egress policy, crawler SSRF protection, and
  configurable query-logging privacy modes.
- **Operations.** Metrics, structured events, backup and restore, and packaging
  for self-hosting.

## Non-Goals

- A drop-in replacement for the Java YaCy Search Server.
- Cloning Java YaCy administration pages (`/*_p.html`).
- Requiring a JVM, Solr, Lucene, or Kelondro runtime.
- Requiring an external Tavily service.
