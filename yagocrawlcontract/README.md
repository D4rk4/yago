# yagocrawlcontract

`yagocrawlcontract` defines the messages shared by `yago-node` and
`yago-crawler`. The current data flow separates search and document storage from
crawl selection:

```text
yago-node -- ReadSearchDemand(cursor) --> yago-crawler
yago-node <-- StreamDocumentMutations(batch, acknowledgement) -- yago-crawler
yago-node <-- Heartbeat and ReportProgress -------------------- yago-crawler
```

Search-demand pages contain aggregate topic and canonical-URL impressions,
clicks, and focus times. The crawler owns how those signals become frontier
priority, retries, recrawls, and fetch decisions. The node accepts authenticated
document upserts and removals and acknowledges a mutation after ingest finishes.
The crawler retries mutations from its durable outbox until acknowledged.

The data exchange requires an Admin API key with the `crawl:write` scope.
Heartbeat and runtime-policy messages report crawler activity and deliver
operator controls. They do not assign URLs to workers.

Progress reports carry cumulative fetched and indexed totals plus document
candidates, removal mutations, pages fetched without a mutation, node rejections,
pending ingest, and bounded outcome reasons. Older workers leave the added
fields empty; the node treats their fetched-but-unindexed pages as unknown.

The older `CrawlOrder`, lease, settlement, and order-progress messages remain in
the protobuf contract for compatibility with stored protocol definitions. The
current node does not open a crawl-order queue, and the current crawler does not
consume the order stream. New crawl demand uses `ReadSearchDemand`; fetched
content uses `StreamDocumentMutations`.

Generated protobuf files under `crawlrpc/` come from
`proto/crawlexchange.proto`. `search_demand.go` and the ingest codecs define the
bounded JSON payloads carried by the exchange.
