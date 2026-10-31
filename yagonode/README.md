# YaGo node

The `yago-node` process serves the local search index, YaCy-compatible peer
endpoints, and the public search APIs.

## Progressive local search

Eligible first-page plain queries return a useful randomized shard sample as
soon as one is available. Responses identify sampled and fully searched shards
so clients can distinguish partial coverage from a complete empty result. A
bounded full-shard search then continues for that query. Only complete results
are retained in the existing five-minute, 32 MiB in-memory session cache; a
process restart clears those cached responses.

Browser clients can consume updates through the YaCy-compatible
`/yacysearch.ndjson` endpoint. The HTML search page updates as those results
arrive. Tavily-compatible `/search` remains a single-response endpoint with
the documented Tavily shape. Exact and later-page searches retain their
existing behavior.

The randomized shard position is an efficiency technique, not a statistical
guarantee of uniform sampling over matching documents. See
[`doc/adr/0085-randomize-deadline-bounded-shard-retrieval.md`](doc/adr/0085-randomize-deadline-bounded-shard-retrieval.md)
and
[`doc/adr/0086-return-useful-shard-samples-and-cache-complete-searches.md`](doc/adr/0086-return-useful-shard-samples-and-cache-complete-searches.md)
for the design and resource bounds.

Run the browser scenarios from [`ui-tests`](ui-tests/README.md).
