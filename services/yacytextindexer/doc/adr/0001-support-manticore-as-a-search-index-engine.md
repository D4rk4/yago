# 1. Support Manticore as a search-index engine

Status: accepted

## Context

`yacytextindexer` writes crawled pages into an operator-run full-text search
index, and the SearXNG search engine reads them back. Until now that index was
always Elasticsearch. Elasticsearch is heavy: a single node reserves hundreds of
megabytes of heap before it holds any documents, which is a poor fit for the
small self-hosted deployments this project targets.

Manticore Search is a general-purpose full-text engine with an empty footprint
of roughly forty megabytes. It supports mutable replace-by-identity, per-field
weighting, and highlighted fragments — the whole shape this service and the
SearXNG engine need. It is packaged as a single container image and speaks a
JSON HTTP API.

## Decision

Support Manticore as an alternative search-index engine alongside Elasticsearch,
selected per deployment. Operators keep Elasticsearch by default; those who want
a lighter stack set the engine to Manticore.

The write side speaks Manticore's native `POST /replace` with a numeric document
identity derived from the page's canonical URL. The search side (the SearXNG
engine) speaks Manticore's `match` query with per-field weights and its
`highlight` node. Neither engine's vocabulary leaks past the adapter that speaks
it.

## Consequences

- Operators gain a lightweight index option without losing Elasticsearch.
- The service and the SearXNG engine each carry two adapters behind one
  engine-neutral seam; a third engine is another adapter, not a rewrite.
- Manticore is GPLv3; it runs as a separate service the operator provisions, so
  its licence does not reach this project's code.
- Relevance ranking differs between the two engines; operators choosing Manticore
  should confirm result quality against their own corpus.
