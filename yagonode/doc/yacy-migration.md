# YaCy document migration

`yacy-migrate` provides a bounded, resumable first step for importing indexed
YaCy documents into a Yago node. It uses YaCy's read-only Solr `select` cursor
API and Yago's authenticated `POST /admin/index/import` route. It does not read
or write either engine's on-disk index files.

## Migration stages

1. **Documents first.** Export and import indexed page content, metadata, links,
   and source status into Yago's document store and local search index. Reconcile
   counts and representative searches before proceeding.
2. **Peer-index records second.** Rebuild local RWI postings and URL metadata
   from the imported documents using the resumable backfill below. This restores
   Yago's peer-exchange data without another fetch, but is a reconstruction, not
   a copy of YaCy's original posting files.
3. **Crawl state separately.** Import only a verified pending-queue snapshot and
   explicit due schedules. Do not replay broad or completed seed lists. The
   current queue snapshot importer does not recover the focused-crawler sidecar
   or exact per-URL next-visit state.
4. **Cutover only after validation.** Confirm document counts, searches, RWI and
   URL metadata, queue state, storage headroom, and peer behavior. Keep the
   source data and existing DHT controls unchanged until that gate passes.

HTTP(S), FTP, and SMB indexed identities are retained. Only HTTP(S) records can
carry an active Yago recrawl schedule; other source freshness values remain
metadata because Yago's recrawl worker is HTTP(S)-only.

Legacy YaCy HTTP URLs with percent-encoded whitespace, a narrowly recognized
invalid host character, or a malformed percent escape in the host, path, or
query are
retained verbatim, tagged in
`yacy.migration_url_status`, and never recrawled. These narrow exceptions
preserve the source identity and document without pretending the malformed
host is fetchable. Other malformed URLs still stop the export.

Document URL identities are limited to 2,048 bytes, matching the persisted-key
format accepted by the previous Yago release. This keeps rollback compatible.
YaCy rows with longer primary or canonical identities are rejected and reported
by the exporter instead of being imported into a store that the previous
release cannot reopen.

When an indexed URL, canonical URL, source ID, or redirect contains URL
userinfo, the adapter removes that userinfo before writing the interchange
record, marks `yacy.migration_credentials_redacted=true`, and suppresses
recrawl for the affected primary URL. Page content and the credential-free
identity remain importable; credentials are never copied to Yago or emitted in
export errors. Credential-bearing link targets are omitted.

YaCy titles longer than Yago's ordinary title field are kept in full in the
`yacy.title_full` metadata value and marked `yacy.migration_title_truncated`;
the searchable title field contains a UTF-8-safe prefix within Yago's normal
limit. Titles larger than the migration metadata budget (8 MiB per record)
remain a hard export error rather than being discarded.

Headings larger than YaGo's typed heading size or count limits are stored as
UTF-8-safe indexed prefixes when they fit and retained in full, with their
original YaCy field and array position, in bounded `yacy.headings_full`
metadata. The record is marked with `yacy.migration_headings_truncated`. If the
complete heading metadata would exceed the 8 MiB per-record migration metadata
budget, export stops rather than dropping it.

Each imported record retains up to 131,072 outbound URLs and 65,536 inbound
URLs. When a source list exceeds its migration bound, Yago stores the bounded
prefix and records available and stored counts in
`yacy.migration_links_truncated`. This keeps one document bounded while making
the loss of excess link entries explicit; ordinary crawler link limits do not
change.

This is a document-content migration plus a separate recoverable snapshot of
pending local crawl requests, not a complete YaCy URLDB or peer-index
migration. The pending frontier can be imported into Yago's durable crawl-order
queue after document import and reconciliation; the focused-crawler sidecar,
full URLDB outcomes, and exact per-URL next-visit schedule are not available
through these interfaces. Imported documents are locally stored and searchable
through Yago's normal document/index owners. The optional RWI backfill described
below rebuilds local YaCy-compatible postings from the imported text; it does
not copy YaCy's original postings or establish complete peer-index equivalence.
Do not cut over or stop YaCy based only on document import, frontier recovery,
or a generated RWI pass. See [the design and field-gap note](yacy-migration-design.md)
and [the RWI backfill design](yacy-rwi-backfill-design.md).

## Export

Build/run from the `yagonode` module. Use a local YaCy endpoint or a protected
TLS tunnel; Basic authentication over an untrusted network is not safe. Keep
the output and checkpoint in private storage because documents may contain
non-public URLs or text.

```sh
cd yagonode
YACY_USERNAME='admin-user' YACY_PASSWORD='read-only-password' \
  go run ./cmd/yacy-migrate export \
    --solr-url 'http://127.0.0.1:8090/solr/select?core=collection1' \
    --source-id 'local-yacy-export-2026-10' \
    --output '/secure/private/yacy-documents.jsonl' \
    --rows 16
```

The endpoint must be a read-only Solr `select` URL. `--rows` is limited to
1–256. The default is 16. Responses are decoded with an 80 MiB bound; if an
unusually large page exceeds it, resume from the checkpoint with a smaller
`--rows` value. `--dry-run` asks Solr for the current row count without
creating an output or checkpoint:

```sh
cd yagonode
go run ./cmd/yacy-migrate export \
  --solr-url 'http://127.0.0.1:8090/solr/select?core=collection1' \
  --source-id 'local-yacy-export-2026-10' --dry-run
```

An export writes a versioned JSONL header and records. Each committed Solr
cursor page is synced before the checkpoint advances. On restart, rerun the
same command with the same source ID and paths; only bytes after the last
committed output offset are discarded. The cursor is not a point-in-time
snapshot: writes or deletes during export can make one pass inconsistent. Run
a fresh export/reconciliation pass and compare counts before any migration
cutover. Use a new output path for each reconciliation pass.

For a long export, the importer can consume only pages already committed by the
export checkpoint and wait for subsequent pages. This lets import overlap
export without reading partial output:

```sh
cd yagonode
YAGO_API_KEY='admin-write-key' \
  go run ./cmd/yacy-migrate import \
    --input '/secure/private/yacy-documents.jsonl' \
    --follow-export-checkpoint '/secure/private/yacy-documents.jsonl.state.json' \
    --endpoint 'https://yago.example/admin/index/import'
```

The follower exits only after the export checkpoint is marked complete and the
imported byte offset reaches its committed offset. Its own import checkpoint
supports restart; resume with the same input, destination, export checkpoint,
and import checkpoint.

## Import

The Yago destination must be running with its crawler/ingest runtime enabled.
Create a bearer API key scoped to `admin:write`, then import over a trusted
network or TLS:

```sh
cd yagonode
YAGO_API_KEY='admin-write-key' \
  go run ./cmd/yacy-migrate import \
    --input '/secure/private/yacy-documents.jsonl' \
    --endpoint 'https://yago.example/admin/index/import'
```

Validate without contacting Yago or writing an import checkpoint:

```sh
cd yagonode
go run ./cmd/yacy-migrate import \
  --input '/secure/private/yacy-documents.jsonl' \
  --endpoint 'https://yago.example/admin/index/import' --dry-run
```

The importer holds a bounded batch (at most 256 records and below the existing
ingest request limit), accounts for exact encoded request size without
re-encoding earlier records, waits for normal ingest backpressure, and
checkpoints only after it receives a valid receipt for every record. Use
`--batch-records` to lower work per request when diagnosing destination backpressure;
`--max-records` bounds one invocation while leaving a resumable checkpoint.
On interruption, rerun with the same input and endpoint. The default checkpoint is
`<input>.import.state.json`; `--state` selects another path. Keep the input
unchanged. A changed size or modification time is rejected rather than
resumed against stale progress.

Before a large import, size the destination's existing `YAGO_STORAGE_QUOTA`
setting for the expected corpus and available disk headroom. The default is
1 GB; reaching it can defer ingest and return HTTP 503 without advancing the
import checkpoint. Resume after increasing the quota or otherwise relieving
the measured storage pressure. Do not point Yago at YaCy's data or index path.

Replaying a batch is idempotent: the normalized/canonical URL and content hash
are checked before ingest. Matching documents return `already-present`; a
different destination document is reported as `conflict` and is not replaced.
The bounded request checks its unique normalized URLs together, and repeated
URLs within the same request reuse the first result. This avoids a separate
document-store read transaction for every source row without weakening conflict
preservation.
The importer preserves separate URLs even when their text hashes match, so
aliases and link evidence are not discarded. Source rows without a supported
required field or beyond Yago's existing document limits stop the pass with an
error; the tool does not truncate or silently skip them.

## Rebuild local RWI from imported documents

YaCy's document export does not include its original peer postings. Yago can
recreate its own YaCy-compatible RWI postings and URL metadata from the
already-imported document store. This is a bounded, resumable derived-data pass;
it does not run a crawler, re-fetch URLs, modify stored documents, update Bleve,
or create recrawl work. It uses the crawler's tokenizer and common artifact
builder, so regenerated postings follow Yago's current rules rather than
claiming byte-for-byte reconstruction of YaCy's index.

Create an API key with `admin:read` and `admin:write` scopes for status and
control. Start with status and a bounded real pilot after backing up and
verifying the Yago data directory:

```sh
cd yagonode
YAGO_API_KEY='admin-read-write-key' go run ./cmd/yacy-migrate rwi-backfill \
  --endpoint 'https://yago.example/admin/index/rwi-backfill' --action status
YAGO_API_KEY='admin-read-write-key' go run ./cmd/yacy-migrate rwi-backfill \
  --endpoint 'https://yago.example/admin/index/rwi-backfill' \
  --action start --maximum-documents 1000
```

Poll `--action status` and inspect generated row counts, estimated encoded row
bytes, local search latency, storage growth, and the node's RWI and URL-metadata
counts. The estimate is not actual disk growth. A pilot stops at its document
limit and leaves a resumable checkpoint. Continue it with `--action resume`; a
zero maximum means continue without a per-run document limit. The node also
resumes a persisted running pass after restart. A completed pass can be
explicitly started again to include documents that arrived during the previous
scan; receiver writes are idempotent.

Do not enable or modify DHT transfer as part of this operation. Rebuilt local
RWI data is peer-exchange material, but network distribution remains governed
by the node's existing DHT controls. Preserve YaCy and its data until a separate
reconciliation verifies imported document counts, searches, metadata, queue
state, and the required peer behavior.

YaCy's current Solr export has no exact next-visit schedule. A versioned JSONL
record may carry `nextVisitAt` when an operator has a separately verified
source for it. Only such an explicit value creates a durable Yago recrawl
schedule, and only a due schedule enters the recrawl frontier. Other imported
records are not added to new-crawl work. Schedules for eligible records in one
import request are committed together in one durable frontier transaction.
The import checkpoint advances only after both document receipts and schedule
storage succeed, so retrying a partially completed request remains idempotent.

## Field mapping and known gaps

The exact stored-field projection, mapping, size limits, source paths, and
missing crawl-state details are recorded in
[`yacy-migration-design.md`](yacy-migration-design.md). In particular, the
available export does not provide full aliases, URLDB status, pending crawl
work, focused sidecar state, anchor text, or exact next-visit times. Yago's
current document model has no typed collections, so YaCy collection and focused
policy values are retained as namespaced document metadata. Peer-facing RWI
postings are engine-specific; the optional derived-data backfill is described
in [`yacy-rwi-backfill-design.md`](yacy-rwi-backfill-design.md).

The current count from a live Solr probe is not a migration guarantee. Verify
destination counts, representative local searches, metadata, URLs and any
supplemented schedules independently before deciding whether the remaining
source-state gaps are acceptable. Keep YaCy and its production data untouched
until that verification and a separate recoverable cutover plan are complete.

## Pending frontier import

The frontier import is a separate step and must run only after the document
import has reached a stable source boundary. The command uses the stopped-YaCy
queue snapshot JSONL plus its crawl-profile sidecar; it never reads or mutates
the live YaCy queue files. Keep both artifacts private because they contain
operator crawl URLs.

First validate the full frontier without contacting Yago or writing a
checkpoint:

```sh
cd yagonode
go run ./cmd/yacy-migrate import-frontier \
  --input '/private/yacy-queue-frontier.jsonl' \
  --profiles '/private/yacy-crawl-profiles.jsonl' \
  --dry-run
```

For a live import, use an API key scoped to `admin:write` and a protected
operations endpoint. The command streams at most 256 requests per bounded
batch, defaults to a sidecar checkpoint next to the input, and resumes only
when both input files and the destination endpoint match the checkpoint. A
batch is acknowledged by a content-derived idempotency key before its byte
offset is checkpointed. `--max-records` is useful for a small test before the
full frontier pass.

```sh
cd yagonode
YAGO_API_KEY='admin-write-key' go run ./cmd/yacy-migrate import-frontier \
  --input '/private/yacy-queue-frontier.jsonl' \
  --profiles '/private/yacy-crawl-profiles.jsonl' \
  --endpoint 'https://yago.example/admin/crawl/frontier/import' \
  --max-records 512
```

The node checks its existing document store before queue publication and skips
already-indexed URLs. The current interchange has no exact source next-visit
time, so these skipped rows are not reintroduced as recrawls. Import stores
orders only; it does not start a crawler worker. The captured production queue
was the ordinary `LOCAL` lane and its source profile was not the Canadian
focused profile. It must not be described or enabled as a Canadian crawl.
