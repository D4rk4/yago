# 11. Expose node metrics through the Prometheus client

Date: 2026-06-23

## Status

Accepted

## Context

The node exposes operational metrics on `/metrics`. Until now this surface was served by the
standard library `expvar` package as a flat JSON document. We need three per-endpoint views:
how many times each HTTP endpoint was called, the response status codes per endpoint, and
execution-time percentiles per endpoint.

`expvar` has no histogram or summary instrument: it exposes only integers, floats, and maps
through a `String()` method. Percentiles would have to be computed by hand from a bucket
structure we maintain ourselves, and the result would still be a bespoke JSON shape that no
standard tooling reads. Latency percentiles in particular are most useful when aggregated
across nodes, which a per-instance precomputed value cannot do.

## Decision

We use `github.com/prometheus/client_golang` and serve `/metrics` in the Prometheus text
exposition format.

- Per-endpoint request outcomes are a `CounterVec` labelled by endpoint and status code.
- Per-endpoint latency is a `HistogramVec` labelled by endpoint; its `_count` series doubles as
  the per-endpoint call count, so we keep a single source of truth for call totals. Percentiles
  are derived at query time with `histogram_quantile()`.
- The endpoint label is the matched `http.ServeMux` pattern. Unmatched requests collapse to a
  single `unmatched` label so scanners cannot inflate label cardinality.
- Storage usage moves from `expvar` to a Prometheus `GaugeFunc`, so `/metrics` is a single
  Prometheus surface and `expvar` is dropped.

## Consequences

- New runtime dependencies, pinned in `go.mod`: `github.com/prometheus/client_golang` and its
  transitive `client_model`, `common`, and `procfs` packages.
- The `/metrics` payload changes from `expvar` JSON to Prometheus text. Any consumer that parsed
  the old JSON must be updated. The previous global `http_responses` map is removed; its data is
  superseded by the per-endpoint `http_requests_total` counter.
- Metrics now aggregate naturally across nodes in a Prometheus server, and latency percentiles
  are computed from histogram buckets rather than maintained per instance.
