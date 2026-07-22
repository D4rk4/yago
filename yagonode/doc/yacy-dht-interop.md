# YaCy DHT interoperability notes

Operational checklist for making a YaCy-compatible peer participate in YaCy DHT
index transfer. The focus is runtime readiness and YaCy sender behaviour, not
field-by-field endpoint definitions.

---

## Receiver baseline

A receiver that wants YaCy peers to treat it as DHT-capable needs:

1. a stable 12-character peer hash;
2. a seed advertising reachable host and port information;
3. `hello.html` returning `yourip`, `yourtype`, and `seed0` for valid callers while rejecting self-pings and callers using the receiver's peer hash;
4. `query.html object=rwicount` returning a numeric `response`; original YaCy
   treats `iam` and `youare` as opaque form values, requires `youare` to equal
   the target hash, and tolerates a missing `iam` field; per-word `rwiurlcount`
   uses `env=<wordhash>`, `lurlcount` is the stock constant `1`, and an unknown
   object or failed per-word index read renders the unresolved template marker;
5. `transferRWI.html` enforcing `network.unit.name`, required transfer fields,
   `youare`, and a persisted sender identified by `iam`, accepting at most 1,000
   declared and actual `wordHash{row}` entries, and reporting missing URL hashes
   with `unknownURL`;
6. `transferURL.html` enforcing `network.unit.name` before target handling and
   requiring the persisted `iam` sender before storage, parsing at most 1,000
   declared `url0..urlN` metadata rows, and rejecting a larger count before
   per-row allocation;
7. `search.html` returning `joincount`, `count`, and `resourceN` rows when remote
   search is supported. Required, excluded, and requested-abstract fields retain
   at most 32 hashes each, and the URL allowlist retains at most 128 hashes. Each
   resource response copy carries an enhanced-base64 `wi` with the complete
   fixed-order 20-column `WordReferenceRow`; stored URL metadata is unchanged.

To receive index transfer, the seed `Flags` field must set bit 2
(`FLAG_ACCEPT_REMOTE_INDEX`). Without that bit, YaCy's sender-side DHT target
selection skips the peer even if its `PeerType` is `senior`.

Network-unit authentication interoperates with the default `freeworld` unit and
same-name peers. Controlled private networks may select Java YaCy's
`salted-magic-sim` calculation with one nonempty shared secret. In that mode the
node signs outbound peer requests with a fresh bounded salt and validates
inbound requests against the authenticated peer identity. Every participating
peer must use the same mode and secret.

At startup the node binds every configured peer, operations, and public-search
HTTP listener before it starts peer greeting and announcement loops. A bind
failure closes the HTTP listeners already opened and prevents the presence loops
from starting. The first outbound hello therefore cannot race the advertised
peer HTTP listener into a false `junior` classification.

Public `resource=global` search uses YaCy DHT positions but draws candidates from
the known senior-peer roster, including peers that have not completed an inbound
callback. This matches YaCy remote search and lets a node behind NAT search the
swarm; an unreachable candidate becomes a partial failure. Outbound index
distribution remains stricter and uses confirmed reachability. Search targets
must pass the age and advertised RWI-inventory gates, and redundant candidates
are sampled randomly within each vertical partition instead of broad-fanning to
the roster. Partition coverage is retained before redundant candidates: with the
freeworld exponent `4`, one eligible peer for each of the 16 vertical partitions
is kept inside the default 16-primary-call and 32-total-call limits.

The same query starts at most 32 physical HTTP attempts even when one peer
advertises several addresses. Each remaining address receives a share of the
remaining aggregate deadline. A connection or response-body I/O failure may
continue to the next address; a size, parse, abstract, or WordReference failure
is protocol evidence and does not. Primary resources, explicit index abstracts,
and secondary metadata calls feed one request-scoped lifecycle reduction: any
successful response wins over a transport failure for that peer, while invalid
protocol data changes reputation without cooling the peer.

A multiword primary search carries stock YaCy `abstracts=auto`. Exact secondary
recovery consumes the abstracts returned by those primary calls and does not
issue separate per-term abstract probes. The intersected URL set produces at
most one stock-compatible secondary request per peer, carrying the term hashes
and URL hashes that peer reported. This matches Java YaCy's
`SecondarySearchSuperviser` call shape and remains within its short repeated-call
throttle. Morphology expansion keeps its separately bounded explicit abstract
work under the same total call budget.

Every received resource must carry an enhanced-base64, fixed-order 20-column
`wi` WordReference whose URL hash equals the resource URL hash. The complete
peer response is rejected before resource counters, result conversion, ranking,
cache warming, or crawl seeding when any row fails that check. The failure is
recorded as invalid peer evidence rather than successful traffic.

Outbound remote-search requests identify the local seed with matching `myseed`
and `iam` values on open and authenticated networks. They carry the directly
representable language, site host, author, file type, URL filter, and preference
fields. The public search request has no stock wire field for its independent
in-URL or top-level-domain operators and no protocol field, so those constraints
are verified again after retrieval instead of being encoded into a different
regular expression dialect.

An authenticated inbound search may contribute its valid `myseed` as a bounded
potential-peer observation. The seed hash must equal `iam`; only its port and
the trusted transport IP are retained, its local type is forced to `virgin`, and
an existing roster record is never replaced. The candidate is neither reachable
nor eligible for search or DHT routing. The ordinary hello cycle remains the
only path that can verify and promote it.

Inbound RWI search applies author containment, URL-regex, file-extension, and
scheme constraints to at most the bounded joined posting set before selecting
top results. URL regular expressions are limited to 2,048 bytes and compiled by
Go's RE2 implementation, which guarantees linear-time matching. Metadata URL
and author decoding is separately bounded. `prefer`, `profile`, `collection`,
and `timezoneOffset` remain accepted compatibility fields without an RWI-layer
signal.

Validated remote result metadata can enter the operator-controlled asynchronous
document/full-text cache without overwriting local crawls. Unlike Java YaCy,
the node does not persist the response's attached RWI into its local exchange
vault because that row lacks durable peer provenance and expiry. It also does
not use peer `references` as ranking or expansion input; accepting those hints
would let an unverified peer alter query semantics without another evidence
boundary.

Inbound compact seeds remain wire-compatible but are decoded under fixed limits:
32 KiB of plain seed data, 128 properties, 128 bytes per property key, 8 KiB per
generic property or news value, and 256 bytes for the peer name. Bootstrap keeps
at most 4,096 decoded seeds and 16 MiB of detached seed data. Search target
selection reuses a context-aware 4,096-peer/16 MiB candidate snapshot and
invalidates it on roster mutations, so query traffic does not rescan the entire
persistent roster.

Inbound DHT transfer metrics are exposed on the ops listener and observe only
traffic accepted through the YaCy wire endpoints. Local crawler ingest and
local index writes do not increment them. The RWI receiver publishes
`yacy_rwi_received_postings_total`,
`yacy_rwi_rejected_postings_total`, `yacy_rwi_unknown_url_total`, and
`yacy_rwi_ingest_duration_seconds`. The URL metadata receiver publishes
`yacy_url_metadata_received_total`, `yacy_url_metadata_rejected_total`, and
`yacy_url_metadata_reconciled_total`. A URL identity is reconciled once when at
least one newly stored metadata row arrives while its hash remains in a
process-local 65,536-entry FIFO observation set populated by accepted RWI rows.
An already-existing identity releases pending state without incrementing, while
a rejected identity remains pending for retry. The set is a bounded metric
correlation aid, not durable index state: FIFO eviction or restart can omit a
reconciliation increment but cannot change accepted postings or metadata.

The peer seed's cumulative sent and received word and URL tallies include
current in-memory observations. Observations coalesce for one second, then each
changed counter uses an independent single-record transaction. If a later
counter fails, only it and the not-yet-attempted counters remain pending, so an
already committed counter is not replayed. Graceful shutdown drains after HTTP
and background transfer producers quiesce, using a fresh five-second context; a
failed flush remains pending for a later attempt. A process or host crash can
lose all pending observations since the last successful counter flush, including
more than one interval when storage failures persist.

YaCy promotes a requester to `senior` or `principal` only after a successful
callback to the requester's advertised `/yacy/query.html?object=rwicount`. A
failed callback leaves the requester `junior` or potential/disconnected.

The Go node retains that distinction for authenticated inbound hello callers.
A caller must provide a usable advertised endpoint, or an advertised port that
can be combined with the trusted request address. The locally observed callback
result replaces the caller's claimed type. A failed callback stores the caller
as `junior` in the bounded persistent roster, where Admin Network can display
it, but does not place it in the active set. A later successful callback promotes
the same caller; a later failed callback demotes it. Locally junior callers are
excluded from hello seed replies, seed-list export, search candidates, reachable
counts, and DHT targets. They share the existing 4,096-peer reservoir bound and
cannot grow an independent unbounded table.

The callback uses the same aggregate 6.5-second HTTP or 13-second HTTPS-first
budget as stock YaCy and requires a nonnegative RWI-count result. It tries at
most five unique seed hosts in deterministic primary-`IP` then `IP6` order;
attempts divide one aggregate deadline, so an unresponsive address cannot
multiply callback time. IPv6-only seeds remain eligible and IPv6 URL authorities
are bracketed. An unspecified advertised literal is replaced by a usable trusted
transport address or rejected. The host that answers becomes the persisted
primary host. The final caller observation has its own bounded one-second
storage context, so
a callback or client disconnect that exhausts the hello request cannot discard
the `junior` record.

---

## Active peer visibility

`seedlist.xml` is not proof that a peer is active for DHT selection. It can show
recent passive peers and preserve their stored `PeerType=senior` value after they
move out of the connected set.

The roster retains a transport-failed peer in a ten-minute cooling state instead
of deleting it and retries it from the durable 24-hour reservoir. Exact normalized
host-and-port ownership keeps only one claimant routable; a verified peer wins
over unverified gossip, while equal endpoints never define self identity. Legacy
overflow is reduced before serving lookups with one bounded value-selection scan
and key-only deletion pages. Seed-list and name lookup read the same active-first
bounded snapshot, so neither performs an unbounded roster scan on a request path.
Primary peer rows keep the v0.0.20 timestamp-plus-seed encoding. Retry, expiry,
and verification evidence use additive row-and-state-bound metadata; v0.0.20
ignores it, and a later start discards malformed, corrupted, orphaned, or
downgrade-stale metadata under a persisted 4,096-row-per-start cleanup cursor.

For sender-side readiness, check the active network view, for example:

```text
/Network.xml?page=1&maxCount=1000
```

The Go node exposes peer discovery metrics on the ops listener:
`yacy_peer_known_total`, `yacy_peer_active_total`,
`yacy_peer_probe_failures_total`, and `yacy_seedlist_imports_total`. The known
and active series are gauges with the planned compatibility names; the failure
and import series are counters.

---

## Sender-side DHT gates

A YaCy node originates DHT transfer only when all of these are true:

1. it is not under `onlineCaution` from recent local/remote search or proxy use;
2. its local seed is not `virgin` (`junior` is sufficient);
3. `sizeConnected() > 32`, otherwise YaCy searches peers directly instead;
4. `network.unit.dht = true`;
5. `allowDistributeIndex != false`;
6. local `RWICount() >= 100`;
7. no crawl is in progress unless `allowDistributeIndexWhileCrawling` is enabled;
8. the indexing queue is near-empty unless `allowDistributeIndexWhileIndexing` is
   enabled.

`Switchboard.dhtShallTransfer` logs the blocking reason at `FINE`; default
`INFO` logging hides it.

The Go gate evaluator keeps the same sender-side decision as named gate results
with stable reason text. The runtime scheduler feeds these gates from reachable
peer count, local RWI word count, storage capacity, and DHT distribution
environment flags. Like `Switchboard.dhtShallTransfer`, it does not use the
local node's inbound public reachability as an outbound-client gate: a junior or
unconfirmed node can still distribute RWI to reachable eligible peers.

The ops listener exposes the current sender-side gate report at
`/api/admin/v1/network/dht/gates`. The JSON response includes the overall open
state, the first blocking reason, raw gate inputs, configured thresholds, and
each named gate result. Its state separately carries public reachability as a
known/unknown tri-state with source and observation time. Admin Network renders
the same named gates and public-reachability evidence without turning
`Unconfirmed` into `Unreachable` or a DHT blocking reason.

The direct public endpoint self-test calls
`/yacy/query.html?object=rwicount&youare=<local-peer-hash>`. It is eligible as
operational reachability evidence only when `YAGO_PUBLIC_SELF_TEST_URL`
explicitly pins the externally reachable peer base URL. Without that setting,
the automatically derived local or loopback target is not queried and does not
prove public ingress. A failed request from a node to its own pinned advertised
address is still sensitive to a NAT that does not support hairpinning.


Ordinary outbound `/yacy/hello.html` exchanges provide an external observation:
the remote peer reports `senior` or `principal` only after it successfully
back-pings the advertised query endpoint, and reports `junior` when that callback
fails. The node retains these classifications for 15 minutes, keyed by at most
1,024 observer hashes. Only observers whose primary advertised address is a
public IP literal contribute this evidence; a hostname or a private, local, or
reserved address does not. Any current observation is authoritative. At least
one current `senior` or `principal` observation reports reachable. If every
current observation is `junior`, the endpoint reports unreachable and an
explicitly pinned direct probe is not used to override it. A `junior`
observation replaces only the same observer's earlier positive result, so one
observer does not erase another observer's successful back-ping. Only when no
current observer evidence exists may the explicitly pinned direct query run.
A greet transport failure records no self-reachability classification because
failure to reach the observer says nothing about ingress to this node.

YaCy may return the caller's advertised DNS name in `yourip` after its callback
succeeds. The Go client therefore accepts a syntactically valid hostname only
when it matches the hostname in the supplied local seed; arbitrary returned
hostnames remain invalid. A verified response refreshes the responder's current
seed metadata, including advertised RWI, while retaining and promoting the host
that was actually contacted from the bounded advertised host order. The
responder hash must match before that address is persisted, and DNS aliases are
not resolved or treated as identity evidence. The immutable local seed hash is
excluded from persistent roster admission and every projection, so a peer
echoing the caller in its known
seed list cannot create a self row. Endpoint equality alone is not identity:
different peers behind one NAT may share an address.

The Go outbound distributor can run one transfer cycle: evaluate sender gates,
dequeue the largest buffered chunk, and perform the two-phase handoff directly.
It does not require a separate `/yacy/query.html?object=rwicount` request before
transfer; that query remains part of hello callback and explicit reachability
checks. A transport failure stays queued for a bounded retry window. A protocol
rejection is restored to durable outbound selection immediately so another
eligible peer can receive it.

The runtime scheduler snapshots sender gates before selecting stored RWI rows.
If gates are closed, it does not remove postings from local storage for outbound
queue feeding.

The Go outbound retry policy turns handoff transport errors and protocol
rejections into bounded exponential retry delays with jitter. A transferRWI
`pause` is a minimum delay, so a longer local retry delay is preserved.
Successful handoffs clear the peer's retry state. Repeated failed cycles produce
a quarantine decision for the peer. The runtime scheduler honors retry readiness
when dequeuing chunks and records every scheduler receipt in the outbound DHT
Prometheus counters. Successful outbound handoffs confirm the peer as reachable.
A transport quarantine restores the chunk for retargeting, removes the
unresponsive peer from reachable membership, and retains its durable roster
observation in a cooling state. A protocol rejection retains general roster
reachability but clears remote-index capability for the exact failed seed only
when its persisted address still matches the attempted endpoint. A later seed
observation may advertise the capability again after the cooldown.

The Prometheus edge registers outbound DHT counters for batches, postings,
failures, and unknown URL requests (see [metrics.md](metrics.md)). The
runtime scheduler observes each distribution receipt, so these counters reflect
live DHT traffic as stored RWI rows are fed into the outbound queue.

## Sender-side transfer shape

YaCy sends an index handoff in two phases. The sender posts RWI rows to
`/yacy/transferRWI.html`; when the receiver reports hashes in `unknownURL`, the
sender loads the matching local URL metadata rows and posts them to
`/yacy/transferURL.html`. An `ok` RWI response must contain the `unknownURL`
field; a present empty value completes an RWI-only handoff, while an absent
field is a malformed response and leaves the postings recoverable. The RWI
receiver preserves upstream preflight result
strings such as `not authentified`, `missing wordc`, `missing entryc`, and
`missing indexes` before RWI intake. The URL receiver preserves upstream
network-auth failure behavior by returning no transferURL result fields before
target handling. RWI gate saturation returns the parseable HTTP 200
`too high load` answer. RWI row-count, storage, cancellation, or pre-commit
deadline pressure returns HTTP 200 `busy`, whose `pause` is expressed in
milliseconds. After URL parsing succeeds, runtime pressure uses that endpoint's
HTTP 200 not-granted answer. A declared URL count above 1,000 is rejected before
allocating per-row storage.

Before enqueue, the Go sender selects a bounded set of stored RWI postings and
commits the complete restart journal before a separate update removes live RWI
rows. The removal phase starts only after the journal phase succeeds. A failure
after one live shard commits therefore still leaves every selected row in the
journal. Restoration uses the inverse order: live RWI rows commit before a
separate update releases their journal entries. A failed phase may leave a
harmless duplicate, and replay converges idempotently without leaving a posting
absent from both locations. If enqueue cannot safely keep transferable rows,
the sender restores those rows and clears their recovery records. A selected
posting whose URL metadata is known missing is terminally removed together with
its recovery record, matching YaCy's sender-side orphan filter. A
metadata-backed posting with no eligible target is restored instead of being
discarded.

Once redundancy copies are queued, one accepted transfer leaves the recovery
record intact while another in-memory copy remains. Only the final accepted copy
clears it. A process stop before that point therefore restores the posting and
may resend a duplicate, but does not lose it. An `ok` `transferRWI` or
`transferURL` response carrying `errorURL` is split by URL hash: affected
postings have every queued redundancy sibling cancelled before they are restored
for target selection, while unaffected postings complete under the same
final-copy rule. A failed rejected-posting restore remains local-only and is
retried before another handoff, so it cannot become an immediate resend to the
same peer. If the final accepted copy cannot clear the local recovery journal,
the complete confirmation batch likewise remains local-only and is retried on
later distributor cycles without peer-failure attribution or another network
copy. A transport error retries the same endpoint only up to the bounded
quarantine threshold. A protocol rejection, or a transport failure reaching
that threshold, restores the rows to durable outbound selection for a later
target choice. All alternate-address `transferRWI` and `transferURL` attempts
share the earlier caller or HTTP-client deadline and divide its remaining time
across candidates. If the process stops with selected rows still pending in the
recovery collection, storage startup restores them to the local RWI index before
the next outbound feed.

## Sender-side target order

YaCy selects DHT distribution targets by computing the word's YaCy DHT position,
walking connected peers in YaCy hash order from that position, wrapping at the
end of the hash ring, and keeping only peers that advertise
`FLAG_ACCEPT_REMOTE_INDEX`. The distribution path also skips peers younger than
three days according to their seed `BDate`. The node's own advertised `BDate`
is stored on first start and survives restarts, so remote YaCy peers judge this
node's age from its real history when they select it as a distribution target.
A freshly started peer therefore receives no DHT transfers from YaCy peers for
its first three days; operators migrating an established peer identity can
declare the original birth date through `YAGO_PEER_BIRTH_DATE`.
The Go selector preserves that
target order and eligibility logic for the peer-routing step. The runtime
defaults to YaCy freeworld senior redundancy `3` and vertical partition exponent
`4`, and operators can override those network-unit values for private networks.
A transfer rejection clears the exact failed seed's remote-index flag only while
the persisted endpoint still shares its address. This prevents a stale response
from overwriting a newer endpoint observation. The rejected chunk is restored
for target selection, and later seed announcements may advertise capability
again after the response pause and local retry delay.

Before transfer, YaCy splits a word's RWI rows by the URL hash's vertical DHT
partition, accumulates each partition into the chunk for its primary target,
drops rows without local URL metadata, caps a chunk at 1000 RWI rows, and
dequeues the largest buffered chunk first. The Go exchange queue preserves that
batch shape. The runtime scheduler feeds an empty outbound queue from stored RWI
selections. A two-local-Go-node integration test covers stored RWI selection,
`transferRWI.html`, `unknownURL`, `transferURL.html`, sender deletion, and
receiver durability. Live stock-Java tests additionally prove Java-to-Yago
transfer, Yago-to-Java two-phase distribution, and Java global search of a
Yago-only RWI result (`TestRealYaCyTransfersRWIToFleet`,
`TestNodeDistributesRWIToRealYaCy`, and
`TestRealYaCyGlobalSearchFindsYagoRWI`). They default to
`docker.io/yacy/yacy_search_server@sha256:4225dd07b605347b62ff1fbfa0268217aa79ba2d29bdb0a76d5366d4267398da`;
`YAGO_YACY_IMAGE` can select another explicitly pinned test image.

Yago computes a URL identity without a DNS lookup. Stock Java YaCy can classify
an unresolvable hostname under an unrecognized top-level domain as local, which
changes the domain flag in its URL hash. Cross-peer identities therefore match
for the recognized public-domain fixtures covered by the live matrix, while an
unresolvable synthetic hostname under an unknown top-level domain can differ.
Adding DNS to the hashing path would make identity depend on transient network
state and is deliberately avoided.

---

## Dispatcher startup caveat

YaCy creates `Switchboard.dhtDispatcher` during startup or network relocation
only when `peers.sizeConnected() != 0`. If YaCy boots with an empty connected-peer
database, the dispatcher stays `null`.

Later `hello.html` handshakes can add active peers, but peer arrival does not
recreate the dispatcher. While it is `null`, `dhtTransferJob()` returns without
distributing RWI. Persist at least one active connected peer and restart YaCy so
the next boot constructs the dispatcher.
