# YaCy document migration design

## Source and destination paths

YaCy exposes its embedded Solr `select` API for read-only document export.
The source index and its storage remain owned by YaCy; Yago never mounts or
opens those files. A real limited export/import pilot verified the supported
read and ingest paths without changing the source index.

YaCy's pack generator can write Elasticsearch bulk JSONL, but that endpoint
forces a minified field set and filters to HTTP 200 documents. `IndexExport_p`
exports URLs or text, not a complete document stream. The implementation uses
YaCy's read-only Solr `select` endpoint with cursor pagination and an explicit
stored-field list; it does not start an export job or write under the YaCy data
directory. Runtime probes confirmed `cursorMark` support. The reported count
changed between live probes, so the cursor is not a point-in-time snapshot. No
document values are retained in the repository. The embedded-Solr dump/restore
page is not an interchange format and is not used.

The focused-crawler sidecar is under
`DATA/SETTINGS/focused/url-policy-metadata.heap`. It stores policy IDs and
versions, relevance, priority, collections, reason codes, depth, per-host
budget, recrawl interval, and novelty class. It has no documented bulk export
interface and does not store a per-URL next-visit timestamp. YaCy's URL metadata
and crawl queues are separate from the full-text export.

YagoSeek's destination owners are `yagocrawlcontract.DocumentIngest`,
`yagonode/internal/crawlresults`, `yagonode/internal/documentstore`, the search
index, URL metadata, and `yagonode/internal/recrawlfrontier`. The feature adds a
scoped `POST /admin/index/import` endpoint to the existing admin listener. It
submits bounded documents through the normal crawl-result consumer and its
document-store/search-index owners; it does not open Bleve or bbolt files
directly. Pending queue recovery uses `POST /admin/crawl/frontier/import` and
the existing durable crawl-order queue. Both routes inherit admin
authentication and require an `admin:write` API key for bearer-token clients.

## Versioned record and field mapping

The command `yagonode/cmd/yacy-migrate` reads the existing embedded Solr
`select` endpoint using a stable `id` sort, `cursorMark`, and bounded `rows`; the
operator supplies the endpoint including `core=collection1`. It writes
versioned JSONL with a format header and one document record per line. Stable
record IDs derive from the non-sensitive export ID and YaCy's source document
ID (or normalized URL when the source ID is absent). The SHA-256 content hash
is computed over extracted text because YaCy's `exact_signature_l` is not a
cryptographic hash. The initial stored-field projection carries:

| YaCy export field | Yago destination |
| --- | --- |
| `sku` | source URL and normalized URL input |
| `canonical_s` | canonical URL |
| `title`, `h1_txt`–`h6_txt`, `text_t` | title, headings, extracted text |
| `language_s`, `content_type`, `httpstatus_i`, `failreason_s`, `load_date_dt`, `last_modified` | language, MIME type, fetch status, fetch time, failure status, modified time |
| `exact_signature_l` | retained as source metadata; it is not a cryptographic content hash |
| `outboundlinks_protocol_sxt` + `outboundlinks_urlstub_sxt` | outlink URLs; YaCy stores only non-HTTP protocols as sparse `<three-digit-index>-<protocol>` overrides, with HTTP implicit. Anchor text is not paired with links because the selected stored fields do not provide a reliable URL-to-text mapping. |
| `inboundlinks_protocol_sxt` + `inboundlinks_urlstub_sxt` | URL-only inlink records using the same sparse protocol-override encoding |
| `collection_sxt`, `focused_policy_sxt`, `focused_profile_version_sxt`, `focused_relevance_i`, `focused_priority_s`, `focused_reason_sxt`, `httpstatus_redirect_s` | namespaced source metadata; canonical URL is also mapped to its typed field |
| `fresh_date_dt` | retained as source metadata and used as a derived Yago due time, not an exact next-fetch time; YaCy defines it as the freshness cutoff and its default recrawler selects values at least a day past that cutoff ([schema](https://yacy.de/dev/solr-schema/), [recrawler](https://github.com/yacy/yacy_search_server/blob/master/source/net/yacy/crawler/RecrawlBusyThread.java)) |

When no source content hash is available, the adapter computes SHA-256 over the
extracted text. YaCy's 64-bit `exact_signature_l` is not treated as equivalent.

Real source rows exposed legacy YaCy URLs whose HTTP hosts contain
percent-encoded whitespace or another invalid host character. YaCy retained
those values, but Go's URL parser cannot treat them as network locations. The
adapter preserves their normalized and canonical strings verbatim and marks
them with `yacy.migration_url_status`;
`POST /admin/index/import` accepts only these narrowly recognized legacy forms,
and the importer never schedules them for recrawl. They remain searchable
document identities, not claims that malformed hosts are fetchable. Other
malformed or unsupported URL rows still fail rather than being rewritten or
silently dropped.

Document URL identities remain limited to 2,048 bytes, matching the persisted
key format accepted by the previous Yago release. This keeps rollback safe.
Longer primary or canonical identities are rejected during export or import
validation rather than written into a store that the previous release cannot
reopen.

If a primary URL or canonical URL contains URL userinfo, the adapter strips
userinfo, records `yacy.migration_credentials_redacted=true`, and disables
recrawl for that record. It also sanitizes redirect/source-ID URL metadata and
omits credential-bearing link targets. This avoids copying authentication
material into the destination while retaining the page content. Such a URL's
identity is necessarily credential-free rather than byte-identical to YaCy's
stored spelling.

Titles beyond Yago's ordinary 4 KiB title limit are preserved verbatim in the
bounded `yacy.title_full` metadata value. The ordinary searchable title is a
UTF-8-safe prefix; the record carries `yacy.migration_title_truncated=true`.
If the full value cannot fit the migration metadata budget, export stops with a
field-specific but content-free error.

Headings longer than the ordinary 2 KiB heading bound, and headings beyond the
4,096 typed-heading limit, use a loss-preserving strategy: YaGo indexes a
UTF-8-safe prefix for values within the count limit and migration metadata
stores each full heading with its source `hN_txt` field and array position.
The record is marked with `yacy.migration_headings_truncated`. This remains
bounded by the migration-only 8 MiB metadata limit; an oversized combined
metadata value stops export rather than silently discarding heading content.

## Gaps and assumptions

- The built-in pack export contains only HTTP 200 records and its minified
  format omits most metadata. The read-only Solr query can include stored
  failure fields, but the URLDB and pending crawl-queue entries remain separate.
- YaCy's full-text export has no complete alias history, per-document
  crawl-profile assignment, pending-queue state, or focused sidecar values.
  `fresh_date_dt` is the supported freshness cutoff used by YaCy's recrawler,
  not a precise per-URL scheduled visit. This first importer uses the cutoff as
  a derived Yago due time, which may be up to a day earlier than the stock
  YaCy recrawler's default threshold; verify/adjust schedules before cutover.
  Other schedules are not inferred from fetch timestamps.
- Webgraph records are stored separately. Only links present in the full-text
  document export can be migrated in this first slice.
- YaCy does not provide full inbound/outbound anchor text in the selected
  stored fields, so link URLs are preserved but link text is not. The document
  importer does not carry YaCy's original peer postings or URLDB state. Yago's
  optional RWI backfill can reconstruct local postings and URL metadata from
  imported text, but it cannot reproduce the original YaCy posting bytes or
  scheduler state. See `yacy-rwi-backfill-design.md`.
- Yago's current document model has no typed collection or alias fields. The
  adapter preserves those source values as namespaced document metadata rather
  than silently dropping them or changing search semantics.
- The ordinary crawler ingest contract remains capped at 1 MiB of extracted
  text, 256 headings, 4,096 outlinks, and 8 KiB of metadata. The migration-only
  import boundary accepts up to
  32 MiB per document and 64 MiB per request. It allows up to 65,536 inbound
  URLs and 131,072 outbound URLs per document while retaining bounded
  collections: up to 4,096 headings, 1,024 outbound anchors, and 8 MiB of
  migration metadata. When a source link list exceeds its migration bound, the
  adapter stores the bounded prefix and records available/stored counts in
  `yacy.migration_links_truncated`; ordinary crawler limits remain unchanged.
  The heading bound accommodates large but valid YaCy table-of-contents pages.
  The exporter fails clearly when required content or metadata cannot fit; it
  does not silently skip a document. Marked malformed URL identities are
  retained verbatim and never recrawled.

YaCy description metadata can exceed the ordinary 8 KiB envelope observed in
small crawl records. A migration-only 8 MiB metadata limit preserves long
source descriptions, including the 4.18 MiB description encountered in the
live export; a record exceeding it stops the export rather than being
truncated. Ordinary crawler metadata remains capped at 8 KiB.
- A row without `fresh_date_dt` is imported as completed content but is not
  scheduled for recrawl. A row with that field gets a durable derived schedule;
  only due schedules are eligible to run. Import itself does not submit URLs to
  the new-crawl queue. The due-time conversion is not exact YaCy scheduler-state
  parity and must be reconciled before cutover.
- Due schedules from one authenticated document-import request are applied in
  one frontier transaction after its document receipts complete. A failed
  schedule transaction leaves the source checkpoint unchanged; retrying the
  request is idempotent.
- Bulk imports preserve source content hashes and skip Yago's derived
  near-duplicate clustering, which profiling showed dominates CPU as the
  corpus grows. YaCy does not provide Yago cluster assignments, so each source
  URL remains its own searchable record. Ordinary crawler ingestion retains
  full clustering; an imported URL participates when later recrawled.

This provides a verifiable document migration and derived schedule preservation
when schedule data is supplied. The Solr document interface alone does not
export YaCy's pending queue or focused sidecar.

## Pending frontier recovery

YaCy persists pending crawl requests separately from Solr documents. The
migration reads `NoticedURL` stacks only from a timestamped queue snapshot,
never from the live production files. YaCy's queue-index implementation opens
its storage read/write even for iteration, so the verified snapshot archive is
kept intact and the reader operates on a separate writable working copy. The
exporter enumerates the concrete stacks and omits aggregate stack aliases to
avoid emitting the same request twice.

The versioned frontier interchange records the URL, stack, crawl-profile
handle, depth, anchor text, initiator bytes, and queued timestamp when present.
The separate profile interchange preserves the source profile's serialized
field map and state. The importer maps the compatible URL filters, depth,
per-host page budget, robots behavior, and format settings into a Yago
`CrawlProfile`, while retaining the original profile values as namespaced
source metadata. Source-only fields stay at the YaCy adapter edge. A referrer
URL is not present in YaCy's persisted `Request` record and is therefore not
invented.

The frontier adapter never transmits URLs containing user information or
URLs that fail Yago's absolute-URL/scheme/length checks. It advances them as
explicit `blockedCredentialURLs` or `blockedInvalidURLs` outcomes in the
checkpoint and reports aggregate counts only. The original private snapshot is
left unchanged for operator review; these blocked records are not claimed as
restored crawl work.

`yacy-migrate import-frontier` reads the queue and profile JSONL artifacts in
bounded batches, reconstructs its URL/profile deduplication set on resume, and
checkpoints only after the node accepts a batch. The receiver verifies a
content-derived batch ID and uses the durable crawl-order queue's idempotency
key, so retrying an accepted batch cannot create another order. Before queue
publication, Yago checks its document store and excludes already-indexed
URLs. This intentionally prefers avoiding duplicate fetches: the Solr export
does not contain the exact YaCy next-visit schedule, so imported URLs already
present in Yago are not re-enqueued as recrawls. Queue import itself does not
start a crawler or fetch pages; a worker must be explicitly connected later.
Duplicate URL/profile pairs are counted and coalesced for dispatch while the
unmodified source interchange remains available for audit.

The imported ordinary YaCy profile is mapped only where semantics are
compatible: URL filters, depth (with YaCy's zero/unbounded depth represented by
Yago's maximum supported depth), per-host budget, and nofollow behavior. The
complete source profile field map is retained in crawl-profile metadata. YaCy
collection assignment, focused-crawler policy state, source URLDB outcomes,
and exact recrawl timing have no compatible typed destination in this slice;
the frontier is therefore recoverable, but not a claim of complete crawl-state
or focused-policy parity. The current production frontier snapshot contains
ordinary `LOCAL` work, not the Canadian focused lane.

YaCy's `recrawlIfOlder` profile field is an absolute millisecond cutoff date;
`Long.MAX_VALUE` represents eternity. Yago's similarly named profile field is
a duration measured after fetch. These values are not interchangeable, so the
frontier adapter keeps YaCy's exact value in `yacy.profile.recrawlIfOlder` and
does not set a Yago recrawl interval from it. Per-URL `nextVisitAt`, when
separately present in the queue interchange, remains the only source for an
exact imported visit schedule.

This is not a full URLDB migration. Solr documents and the pending frontier do
not expose all failed, blocked, alias-history, focused-sidecar, or exact
per-URL next-visit states. Those remain explicit cutover gaps and must not be
represented as successfully migrated.

## Recovery and idempotency

The exporter checkpoints the last committed Solr cursor and output byte offset;
on restart it truncates only its own output artifact to that committed offset
before resuming. The importer checkpoints the interchange file identity and
completed byte offset using atomic replacement and filesystem sync. Export
pages, import HTTP batches, and input lines have fixed upper bounds; destination
ingest backpressure is synchronous. Restarting after a timeout retries the
same batch. Existing normalized-URL/content matches return `already-present`;
different content at the same normalized URL returns `conflict` without
replacement. The normalized URL is the record identity: distinct URL rows are
retained even when they share a canonical URL or identical content, so alias
and link evidence is not discarded. A fresh complete export and
import can be used as a reconciliation pass; use a new output/checkpoint path
and the same stable source ID. Because Solr cursor iteration is live rather than
snapshot-isolated, repeat reconciliation until the source and exported counts
are stable before any cutover. `--dry-run` validates/maps source records without
writing an output or checkpoint and validates import input without contacting
Yago. Import never enqueues an ordinary crawl; only an explicitly provided,
durable `nextVisitAt` schedule can reach the recrawl frontier, and then only
when due.
