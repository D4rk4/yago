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
   requires `youare` and tolerates a missing `iam` field; per-word
   `rwiurlcount` uses `env=<wordhash>`;
5. `transferRWI.html` enforcing `network.unit.name`, required transfer fields,
   and `youare`, accepting at most 1,000 declared and actual `wordHash{row}`
   entries, and reporting missing URL hashes with `unknownURL`;
6. `transferURL.html` enforcing `network.unit.name` before target handling and
   parsing at most 1,000 declared `url0..urlN` metadata rows and rejecting a
   larger count before per-row allocation;
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

Public `resource=global` search uses YaCy DHT positions but draws candidates from
the known senior-peer roster, including peers that have not completed an inbound
callback. This matches YaCy remote search and lets a node behind NAT search the
swarm; an unreachable candidate becomes a partial failure. Outbound index
distribution remains stricter and uses confirmed reachability. Search targets
must pass the age and advertised RWI-inventory gates, and redundant candidates
are sampled randomly instead of broad-fanning to the roster.

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

---

## Active peer visibility

`seedlist.xml` is not proof that a peer is active for DHT selection. It can show
recent passive peers and preserve their stored `PeerType=senior` value after they
move out of the connected set.

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
with stable reason text. The runtime scheduler feeds these gates from the local
YaCy-compatible public endpoint self-test, reachable peer count, local RWI word
count, storage capacity, and DHT distribution environment flags.

The ops listener exposes the current sender-side gate report at
`/api/admin/v1/network/dht/gates`. The JSON response includes the overall open
state, the first blocking reason, raw gate inputs, configured thresholds, and
each named gate result. This is the current machine-readable surface for future
admin UI work.

The public endpoint self-test calls
`/yacy/query.html?object=rwicount&youare=<local-peer-hash>`. By default it uses
the local peer listener with loopback substituted for wildcard listen addresses.
Set `YAGO_PUBLIC_SELF_TEST_URL` to the externally reachable peer base URL when a
reverse proxy or NAT path should be tested instead.

The Go outbound transfer layer can probe a target peer's `rwicount` through
`/yacy/query.html` and treats `response=-1`, missing responses, malformed
responses, and negative values as failed capacity probes.

The Go outbound distributor can run one transfer cycle: evaluate sender gates,
dequeue the largest buffered chunk, probe target RWI capacity, and perform the
two-phase handoff. If capacity probing, transfer, or protocol acceptance fails,
the chunk is returned to the outbound buffer.

The runtime scheduler snapshots sender gates before selecting stored RWI rows.
If gates are closed, it does not remove postings from local storage for outbound
queue feeding.

The Go outbound retry policy turns capacity failures, handoff transport errors,
and protocol rejections into bounded exponential retry delays with jitter.
Successful handoffs clear the peer's retry state. Repeated failed cycles produce
a quarantine decision for the peer. The runtime scheduler honors retry readiness
when dequeuing chunks and records every scheduler receipt in the outbound DHT
Prometheus counters. Successful outbound handoffs confirm the peer as reachable.
A transport or capacity quarantine removes an unresponsive peer from the local
roster. A protocol rejection retains roster reachability and the advertised
remote-index flag because YaCy uses the same rejection values for operator
policy, load shedding, admission pressure, discovery races, and target mismatch.

The Prometheus edge registers outbound DHT counters for batches, postings,
failures, and unknown URL requests (see [metrics.md](metrics.md)). The
runtime scheduler observes each distribution receipt, so these counters reflect
live DHT traffic as stored RWI rows are fed into the outbound queue.

## Sender-side transfer shape

YaCy sends an index handoff in two phases. The sender posts RWI rows to
`/yacy/transferRWI.html`; when the receiver reports hashes in `unknownURL`, the
sender loads the matching local URL metadata rows and posts them to
`/yacy/transferURL.html`. The RWI receiver preserves upstream preflight result
strings such as `not authentified`, `missing wordc`, `missing entryc`, and
`missing indexes` before RWI intake. The URL receiver preserves upstream
network-auth failure behavior by returning no transferURL result fields before
target handling. RWI gate saturation returns the parseable HTTP 200
`too high load` answer. RWI row-count, storage, cancellation, or pre-commit
deadline pressure returns HTTP 200 `busy`, whose `pause` is expressed in
milliseconds. After URL parsing succeeds, runtime pressure uses that endpoint's
HTTP 200 not-granted answer. A declared URL count above 1,000 is rejected before
allocating per-row storage.

Before enqueue, the Go sender selects a bounded set of stored RWI postings in
one storage update, journals the selected rows for restart recovery, and removes
those postings from local RWI storage. If enqueue cannot safely keep
transferable rows, the sender restores those rows and clears their recovery
records. Rows that no longer have local URL metadata remain dropped, matching
YaCy's sender-side filtering.

Once a chunk is queued, successful accepted transfer leaves the selected postings
deleted and clears their recovery records. Capacity failures, transfer errors,
and protocol rejections stay in the outbound queue for retry. If the process
stops with selected rows still pending in the recovery collection, storage
startup restores them to the local RWI index before the next outbound feed.

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
Transfer rejection values never overwrite the seed's remote-index flag. The
failed chunk remains queued behind bounded retry readiness, while later seed
announcements remain the source of target capability.

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
