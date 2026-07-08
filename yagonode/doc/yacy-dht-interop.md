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
   and `youare`, accepting `wordHash{row}` entries, and reporting missing URL
   hashes with `unknownURL`;
6. `transferURL.html` enforcing `network.unit.name` before target handling and
   accepting indexed `url0..urlN` metadata rows;
7. `search.html` returning `joincount`, `count`, and `resourceN` rows when remote
   search is supported.

To receive index transfer, the seed `Flags` field must set bit 2
(`FLAG_ACCEPT_REMOTE_INDEX`). Without that bit, YaCy's sender-side DHT target
selection skips the peer even if its `PeerType` is `senior`.

Public `resource=global` search uses the same DHT target selector as outbound
index distribution. For each query term hash, the node computes YaCy DHT
positions and queries only reachable peers that advertise remote-index intake
pass the age gate, and advertise non-empty RWI inventory. When the DHT position
has more redundant eligible candidates than the configured query fanout will
use, the node samples the target set randomly. Missing DHT targets are reported
as partial search failures instead of broad-fanning the query to arbitrary
reachable peers.

Inbound DHT transfer metrics are exposed on the ops listener. The RWI receiver
publishes `yacy_rwi_received_postings_total`,
`yacy_rwi_rejected_postings_total`, `yacy_rwi_unknown_url_total`, and
`yacy_rwi_ingest_duration_seconds`. The URL metadata receiver publishes
`yacy_url_metadata_received_total`, `yacy_url_metadata_rejected_total`, and
`yacy_url_metadata_reconciled_total`. A URL row is reconciled when metadata
arrives for a URL hash already referenced by stored RWI postings.

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
Prometheus counters. Successful outbound handoffs confirm the peer as reachable
in the local roster, and quarantine decisions remove the peer from the reachable
and known peer sets so target selection stops using it.

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
target handling.

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
When a transfer target rejects the handoff and the current roster entry still
shares the failed target's advertised address, the Go scheduler clears that
peer's remote-index flag so future DHT target selection skips it. The peer can
stay reachable for other P2P operations.

Before transfer, YaCy splits a word's RWI rows by the URL hash's vertical DHT
partition, accumulates each partition into the chunk for its primary target,
drops rows without local URL metadata, caps a chunk at 1000 RWI rows, and
dequeues the largest buffered chunk first. The Go exchange queue preserves that
batch shape. The runtime scheduler feeds an empty outbound queue from stored RWI
selections. A two-local-Go-node integration test covers stored RWI selection,
`transferRWI.html`, `unknownURL`, `transferURL.html`, sender deletion, and
receiver durability. Remaining dispatcher work is end-to-end Java YaCy interop
distribution tests.

---

## Dispatcher startup caveat

YaCy creates `Switchboard.dhtDispatcher` during startup or network relocation
only when `peers.sizeConnected() != 0`. If YaCy boots with an empty connected-peer
database, the dispatcher stays `null`.

Later `hello.html` handshakes can add active peers, but peer arrival does not
recreate the dispatcher. While it is `null`, `dhtTransferJob()` returns without
distributing RWI. Persist at least one active connected peer and restart YaCy so
the next boot constructs the dispatcher.
