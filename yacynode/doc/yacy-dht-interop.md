# YaCy DHT interoperability notes

Operational checklist for making a YaCy-compatible peer participate in YaCy DHT
index transfer. The focus is runtime readiness and YaCy sender behaviour, not
field-by-field endpoint definitions.

---

## Receiver baseline

A receiver that wants YaCy peers to treat it as DHT-capable needs:

1. a stable 12-character peer hash;
2. a seed advertising reachable host and port information;
3. `hello.html` returning `yourip`, `yourtype`, and `seed0`;
4. `query.html object=rwicount` returning a numeric `response`;
5. `transferRWI.html` enforcing `youare`, accepting `wordHash{row}` entries, and
   reporting missing URL hashes with `unknownURL`;
6. `transferURL.html` accepting indexed `url0..urlN` metadata rows;
7. `search.html` returning `joincount`, `count`, and `resourceN` rows when remote
   search is supported.

To receive index transfer, the seed `Flags` field must set bit 2
(`FLAG_ACCEPT_REMOTE_INDEX`). Without that bit, YaCy's sender-side DHT target
selection skips the peer even if its `PeerType` is `senior`.

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

---

## Dispatcher startup caveat

YaCy creates `Switchboard.dhtDispatcher` during startup or network relocation
only when `peers.sizeConnected() != 0`. If YaCy boots with an empty connected-peer
database, the dispatcher stays `null`.

Later `hello.html` handshakes can add active peers, but peer arrival does not
recreate the dispatcher. While it is `null`, `dhtTransferJob()` returns without
distributing RWI. Persist at least one active connected peer and restart YaCy so
the next boot constructs the dispatcher.
