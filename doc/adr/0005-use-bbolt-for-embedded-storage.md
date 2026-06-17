# 5. Use bbolt for embedded storage

Date: 2026-06-18

## Status

Accepted

## Context

The node needs durable local storage for RWI postings and URL metadata. The storage engine must run
on low-resource Linux devices, require no external service, support bounded reads by word hash, and
return backpressure before accepting records it cannot durably retain.

## Decision

Use `go.etcd.io/bbolt` as the first embedded storage engine.

RWI postings are stored under keys composed from the word hash and URL hash. URL metadata is stored
under the URL hash. Distinct URL hashes referenced by RWI postings and stored row counts are tracked
inside the same database.

## Considered alternatives

SQLite was considered because it is mature, durable, widely inspectable, and has strong transaction
support. It was rejected for the first adapter because the current access pattern is ordered key/value
lookup rather than relational querying, and using SQL would introduce schema and migration policy before
the node needs it.

Pebble was considered because it handles large datasets and prefix scans well. It was rejected for the
first adapter because its LSM compaction, write amplification, and disk-full behavior add operational
tuning that is not justified by the current throughput requirements.

Badger was considered for similar reasons. It was rejected because its value log and garbage collection
model add another storage lifecycle to validate before acknowledging inbound records durably.

LevelDB-style Go stores were considered because they provide ordered keys and prefix reads. They were
rejected because they carry the same compaction class of risk as Pebble and Badger while offering a less
compelling maintenance and integration story for this project.

Plain files with custom indexes were considered to avoid a dependency. They were rejected because the
project would need to own crash recovery, compaction, quota handling, corruption handling, and startup
index loading. That conflicts with the requirement to avoid rebuilding the complete index in memory after
restart.

LMDB was considered because it is durable, fast, and ordered. It was rejected for the first adapter
because its Go integration and deployment story is less straightforward than a pure-Go embedded store.

## Consequences

The adapter can satisfy the existing core storage ports with one local database file and transactional
writes. Search can read postings with ordered prefix scans, and ingestion can be idempotent for repeated
word and URL pairs.

bbolt allows one writer at a time. That matches the current preference for integrity and simple
backpressure over ingestion throughput, but a future high-throughput node may need another adapter.
