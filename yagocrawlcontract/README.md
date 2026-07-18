# yagocrawlcontract

`yagocrawlcontract` is the shared message contract between `yago-node` and the
optional crawler service. It exists as a separate Go module so both services can
exchange crawl work and crawl results without importing each other.

The Go types and JSON codec tests in this module are the source of truth for field
names, encoded shapes, and handle calculation. This README describes only the
behavioral contract that is not obvious from the type definitions.

## Message flow

The contract has a leased work flow and a feedback-bearing ingest flow:

```text
          WorkerRegistration + CrawlOrderDelivery(lease ID)
node --------------------------------------------------> crawler
     <---------- AckOrder (ACK/NAK/TERM) + Heartbeat + progress

          SubmitIngest(IngestBatch)
node <----------------------------------------------- crawler
     -----------------------------------------------> accepted/backpressure
```

`CrawlOrder` carries crawl work from the node to crawler instances. The order includes
the crawl profile and seed requests needed to start or continue a crawl.
Its optional priority identifies automatic-discovery work explicitly; an absent
or unknown value is normal work. Queue policy belongs to the node and the crawler
does not derive priority from a profile name. A current crawler retains that
metadata on its run and gives at most three due automatic-discovery pages to its
fetch workers before a due normal page. Selection remains work-conserving, and
existing run fairness and value scoring apply within each class.
Each seed request has a mode. Empty mode and `url` mean a normal page URL.
`sitemap` means an XML sitemap or sitemap index. `sitelist` means a plain text
URL list. Crawlers expand sitemap and sitelist starts into normal URL requests
before frontier admission.

`IngestBatch` carries references back to the node for one fetched page: document
content metadata, bounded image metadata, RWI postings, URL metadata, and the
attribution data needed by the node. Live pages and removal tombstones carry a
stable observation ID and UTC observation time so the node can order separate
deliveries and recognize a committed retry after an acknowledgement is lost.
Older batches without those fields remain accepted: observation time falls back
to `Document.FetchedAt`, and the node derives a stable identity from the batch.
The node persists the latest completed observation per source URL after ingest
side effects and before acknowledging the submission. Before those side effects,
it snapshots the exact worker, process-session, lease, and run authorization and
releases the lease-mutation boundary. A stale submission is rejected before
storage; a successful snapshot authorizes the idempotent absorption without
holding a lease lock across document or index storage.

Multiple crawler processes register distinct worker identities, share the durable
order queue, and publish results to one node. Order settlement and ingest replies
remain bound to the crawler call that initiated them.

The node may return a `set_workers` directive with a bounded per-process
page-fetch concurrency. A current crawler applies it after draining its active
fetch group. A crawler that predates the directive ignores the unknown enum and
continues with its environment bootstrap; a current crawler connected to an
older node receives no directive and also keeps that bootstrap value.

The independent `set_active_runs` directive carries the maximum distinct crawl
tasks one crawler process may keep active. The default is 32 and the valid range
is 1–256. A slot spans prepared-order admission through terminal completion;
waiting ordinary and recovered orders do not activate another frontier or
progress reporter. A lower live value does not preempt an existing task. Older
decoders ignore the directive and retain their environment bootstrap.

A current crawler includes the optional `active_fetches` field in each worker
heartbeat. The value is the number of occupied page-fetch worker jobs from job
start through fetch, parsing, and result publication. An explicit zero is a
measured idle crawler; an absent value identifies a crawler that does not report
this measurement. The node counts only registered order-stream worker identities
and removes their measurement after the last matching stream disconnects. Older
nodes ignore this additive field.

The node may also return `set_automatic_discovery_priority`. The crawler makes
one heartbeat attempt bounded to one second before opening its order stream, so
a successful response applies the persisted node policy before any order enters
the frontier.
If that attempt fails, the crawler retains its environment bootstrap until a
periodic heartbeat succeeds. Existing crawlers ignore the additive directive;
current crawlers connected to older nodes retain their bootstrap policy.

## Provenance

`Provenance` is an opaque node-owned token. The crawler never inspects or changes it;
it only echoes the token on ingest batches so the node can attribute results to the
order source.

Because attribution stays inside that token, local operator crawls and remotely
requested crawls use the same message shape.

## Request hints

`LastModified` on a crawl request is a scheduling hint carried from sitemap
`lastmod` values. It does not make the crawler trust page freshness by itself;
recrawl policy remains a crawler/frontier decision.

## Backpressure

Every streamed order has a durable lease ID. `AckOrder` deletes completed work;
the same call with requeue semantics naks retryable work, while terminate
semantics delete operator-cancelled or permanently invalid work. `Heartbeat`
renews the leases held by one registered worker, and `ReportProgress` carries run
tallies.
The node can durably enqueue more orders than a crawler currently has in its
frontier; crawler saturation is handled by lease ownership rather than by
blocking order creation. Each progress report carries the run's effective
pages-per-minute limit, including the crawler default and an explicit zero for
unlimited dispatch, so the node does not infer worker configuration.

Invalid order modes, URLs, profiles, deterministic fetch responses, and malformed
seed documents terminate the lease. An operator cancellation is also terminal.
Network, server, throttle, timeout, and interrupted expansion failures NAK the
lease into a durable five-second retry delay. Legacy settlement calls are
idempotent for a fixed 24-hour retry horizon: a duplicate terminal ACK succeeds,
while a stale ACK after NAK or expiry is rejected. Rich terminal progress remains
durable through delivery and run-control completion. Once finalized, it keeps the
same 24-hour confirmation window; expiry atomically applies a still-pending
requeue and late valid token confirmation remains idempotent. A live crawler
retries transient settlement failures, while shutdown stops after a bounded
detached attempt and retains unfinished checkpoint state for same-worker
adoption. An expired session-aware lease stays bound to that stable worker;
deferred and legacy sessionless leases remain globally requeueable. The crawler
bounds every heartbeat call to one second and treats an omitted or expired active
lease as an order-stream reconnect signal, allowing immediate same-worker
adoption even when the old stream had no transport failure. The node frames at
most 1,024 adopted leases. Its first recovery frame declares the complete session
lease manifest once, then carries the first ordered batch of at most 16. Every
later batch must be a nonrepeating subset of that manifest. The crawler confirms
exactly the current batch before exposing its first order and requires complete
manifest consumption before ordinary streaming. This keeps replay work linear
while retaining per-order payload streaming and bounded memory.

`WorkerHeartbeat.confirm_active_lease_deliveries` has optional presence. A
current periodic heartbeat sends false: it renews the complete active set but
cannot release unseen delivery credit. A targeted confirmation sends true and
must renew exactly the currently expected lease or recovery batch. An absent
field retains the earlier subset-confirmation behavior for an older crawler.
An older node ignores the field. `CrawlOrderMessage` field 6 carries the complete
recovered-session manifest; the existing field 5 remains the current batch.

Progress delivery is bounded per worker. One in-flight generation is immutable;
later running state for that run may coalesce into one pending replacement, due
terminal reports take priority, and another ready run is served before the
replacement. Intermediate running values may therefore be omitted, while phase
chains and terminal settlement remain ordered.

`SubmitIngest` is unary, so acceptance or retryable backpressure returns directly
to the crawler that submitted the batch. There is no shared feedback topic.

`IngestBatch` JSON is limited to 4 MiB minus 64 KiB of transport headroom, and
the enclosing gRPC message is limited to 4 MiB. The crawler bounds text, URLs,
headings, links, metadata, images, anchors, and postings before submission, then
fits optional collections to the encoded limit. Identity URLs over 2,048 bytes
are rejected, and overlong URL-bearing collection elements are dropped rather
than changed by truncation. The node reports temporary
pipeline or storage saturation as gRPC `Unavailable`; the crawler retries it
with a jittered exponential delay. It also retries the legacy `ResourceExhausted`
saturation code used by older nodes. Current crawler payloads are fitted below
the shared transport ceiling before either retry path can run.

## Crawl profile scope

The profile carries the subset of YaCy crawl settings that affect URL selection and
reference generation, including whether links marked `rel=nofollow` may be followed.
Crawler process settings, such as the browser User-Agent, are not profile fields because
an order cannot safely override process-wide browser identity.

Raw HTML bodies, binary image bodies, media indexing, snapshots, vocabulary
scraping, country and IP filters, HTTP caching, direct document loading, and
onward crawl redistribution are outside this contract.

## Upgrade and rollback

The priority field and worker directive are additive. Existing JSON orders omit
priority and therefore remain normal; protobuf decoders preserve the existing
field numbers and ignore the new directive kind when unsupported. Every pending
JSON payload remains in the established `crawlorders` bucket. Additive secondary
indexes contain only order keys, so an older node ignores priority but still
sees and drains every order in global FIFO order. When the current node returns,
a persisted watermark indexes orders admitted by the older binary and stale
index keys for already consumed orders are removed during selection. The
priority of an unsettled lease created by the older node is recovered from its
retained order payload. The intermediate split-payload development format is
migrated at startup.

The process-session, active-lease, recovered-order framing, lease-bound
ingest/progress, rich terminal settlement, and confirmation-token fields are
additive on the protobuf wire. The recovered-session manifest is field 6 of
`CrawlOrderMessage`; the optional explicit delivery-credit marker is field 9 of
`WorkerHeartbeat`; `set_active_runs` is enum value 8 and its directive value is
field 7. An older crawler ignores these additions and retains legacy subset or
per-order confirmation; a current crawler receiving unmarked replay from an
older node does the same. The current node still requires a valid process-session
identity before accepting a crawler stream or lease mutation. A crawler that
predates that identity is therefore rejected by a current node.
Upgrade the node and every crawler from one release as a coordinated operation:
stop crawlers first, replace both binaries, start and verify the node, then start
crawlers. Rollback requires the matching node broker and crawler checkpoint
backup and the same start order.
