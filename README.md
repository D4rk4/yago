# YagoSeek

<p align="center"><b>Your own federated search engine — one Go binary away.</b></p>

<p align="center">
  <img alt="Go 1.26" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="License AGPL-3.0" src="https://img.shields.io/badge/license-AGPL--3.0-blue">
  <img alt="Status alpha" src="https://img.shields.io/badge/status-alpha-orange">
  <img alt="YaCy protocol" src="https://img.shields.io/badge/YaCy%20P2P-wire%20compatible-6f42c1">
  <img alt="No JS required" src="https://img.shields.io/badge/public%20portal-works%20without%20JS-2ea44f">
</p>

**YagoSeek** is a self-hosted, YaCy-compatible peer-to-peer search node written
in pure Go: run your own web index, join the federated YaCy swarm, crawl the
web with a hardened crawler, and query everything through a Tavily-compatible
Search API, YaCy-compatible endpoints, or a themeable public portal — all
administered from a server-rendered console that works without JavaScript.

**YagoSeek** is the product; **`yago`** is the toolkit — the Go workspace and
its binaries (`yago-node`, `yago-crawler`).

- Project home: https://yagoseek.dev/ · docs: https://docs.yagoseek.dev/
- Source: https://github.com/D4rk4/yago/ — the importable Go modules are listed
  in [`go.work`](go.work)

> [!WARNING]
> **Alpha software.** Everything described below is implemented, covered by
> tests (unit, integration, and containerized end-to-end suites), and runs on
> real nodes — but the project is young and still needs broad, adversarial,
> real-world testing before you should trust it with anything critical.
> Expect rough edges, please report what you hit, and keep backups (there is
> a console page for that now).

---

## ✨ What you get

### 🌐 A real YaCy peer

- Speaks the **YaCy RWI/DHT wire protocol**: hello handshake, seed lists
  (HTML/JSON/XML), inbound and outbound RWI/URL-metadata DHT transfers with
  sender-side gates, remote RWI search, host-link index, peer messages,
  profiles, and shared blacklists — interoperable with the Java YaCy network.
- Stock-Java interoperability is exercised in both directions. A wire handoff
  accepts at most 1,000 rows and returns parseable YaCy HTTP 200 overload
  responses instead of silently acknowledging an unprocessed tail. Remote
  result copies carry the transient `wi` WordReferenceRow evidence required by
  current Java peers, and accepted remote-search resources contribute to the
  advertised received-word and received-URL totals.
- **Swarm participation**: seedlist bootstrap, peer roster with birth-date
  promotion, LAN discovery, peer news, per-peer blocking, and the DHT gates
  dashboard showing exactly why a transfer would or would not fire.
- Deliberate divergences are documented, not hidden — see
  [compatibility.md](yagonode/doc/compatibility.md).

### 🔍 Search that ranks, not just matches

- Local index (sharded [Bleve](https://blevesearch.com/)) + federated swarm
  fan-out + optional operator-enabled web search. The provider is off by default,
  local-only requests never reach it, and the single **Web search fallback
  (DDGS)** selector offers `Disabled`, `Only when requested`, `Enabled on search
  miss`, and `Always`; the last mode starts web retrieval alongside local and
  swarm retrieval. Human-facing surfaces call external results `web`,
  YaCy HTML marks them `[web]`, and Tavily-compatible responses keep their
  standard shape without a provider field. A hyphen or dash inside an ordinary query word
  separates searchable words across local and web retrieval, while a leading
  minus remains an exclusion operator. A web row must independently cover the
  query before it can appear or seed a crawl: one token occurrence cannot stand
  in for several query words, and another language's stopword list cannot weaken
  that check. Results are merged with **reciprocal-rank
  fusion** and **MMR result diversity**. A slow swarm branch cannot discard a
  completed local answer, and a transient refresh cannot replace a recent
  nonempty search session with an infrastructure-generated zero, including when
  the bounded remote-stage admission is full. Completed local, peer, and web
  branches survive a sibling branch's recoverable error or cancellation race.
  An incomplete global request may
  reuse an unexpired equivalent local session without extending its lifetime or
  recording a synthetic global success. Operational search-stage errors retain
  completed rows as a partial answer, and recent paging windows remain readable
  while a deeper page is being extended. Portal navigation links only to the
  materialized result prefix; an explicitly requested page is preserved until
  a complete retrieval proves that it lies beyond the final page.
- **[YagoRank](yagonode/doc/yagorank.md)** — strict and relaxed fielded BM25,
  bounded lexical evidence and RM3, deterministic peer RRF, persistent date,
  anchor, authority, quality, safety, duplicate-cluster, and reputation signals,
  followed by a signed linear LambdaRank or bounded histogram LambdaMART model.
  In a mixed-source result set, learned inference reorders the bounded fused
  top window across local, peer, and web provenance. Each selected document
  inherits its destination slot's bounded relevance scale for final diversity,
  so raw model scores do not compete with the unscored tail.
  Query-clustered and chronological holdouts gate atomic promotion; authenticated
  Team Draft compares complete rankings online, while confidence-filtered
  FairPairs outcomes provide implicit relevance evidence.
  Its console exposes all 13 operator-safe live coefficients: five field boosts,
  authority, freshness, quality, short-URL prior, ordered and unordered
  proximity, lexical blend, and original-gap agreement. Latency windows,
  evidence-confidence rules, safety gates, and learned-model weights remain
  evaluated policy rather than unchecked runtime knobs.
  Its Search Explain panel traces bounded local or global retrieval with
  `local`, `peer`, and `web` provenance, reciprocal-rank contributions, partial
  failures, field evidence, learned signals, and tree paths without adding a
  second provider call.
  Pure Go, CPU-only, no external API, sidecar, model runtime, or YaCy wire change.
- **Language-aware lexical search**: documents route to bounded per-language
  analyzers. Supported inflectional analyzers contribute lower-confidence
  word-form proximity below exact wording; Arabic receives normalization and
  light stemming, and Chinese/Japanese/Korean use mandatory character unigrams
  plus overlapping bigrams. Chinese and Japanese add optional dictionary word
  terms, while equal-length Traditional/Simplified Chinese forms share
  canonical index terms without changing source byte offsets. Dictionary terms
  improve ranking but never gate recall. Hebrew keeps Unicode-normalized
  exact-word proximity without a morphology analyzer. Swarm queries can expand into
  corpus-observed inflections plus bounded forms verified by the supported
  Snowball-rule analyzers; multiword retrieval unions sibling forms within each term and
  intersects across terms without Cartesian peer fan-out. A selected
  cooperating Yago peer can also use the negotiated original requirements for one
  strict bounded analyzer-backed candidate search inside the existing request, so
  a remote-only sibling inflection does not require a second network round.
  Stock YaCy peers remain exact-RWI compatible: every candidate is still an
  ordinary surface hash. Rule-derived forms cover common regular inflections
  absent from the local corpus; analyzer-unconnected irregular
  forms remain outside that compatible bounded expansion.
  Zero-result typo recovery uses bounded analyzer-consistent edit distance
  without document-wide character grams.
- YaCy query operators (`site:`, `inurl:`, `filetype:`, `language:`, `tld:`,
  `author:`, `"phrase"`, `-not`, `near`, `/date`), facet sidebar, content
  verticals (images/audio/video/apps with a lightbox grid), spell-check
  ("did you mean"), zero-result fuzzy recovery, query-term-highlighted
  snippets, anchor-text document expansion, and an explainable ranking API.
  Local snippets and stored-body passages use query-match offsets from the
  indexed language analyzer. A local result with stored-body evidence links its
  cached copy to the matching analyzer-backed range plus bounded surrounding
  context through `/cached`; the
  ordinary full cached-copy link remains available from that passage. Up to the
  first 500 peer, web, and legacy RWI rows run the same bounded analyzer-backed
  visible-field evidence pass while the request context remains live. Invalid
  or empty visible text, unavailable analyzer infrastructure, and rows not
  completed before cancellation or deadline retain bounded structural matching.
  Local and swarm retrieval use parsed bare terms; eligible web search receives
  the bounded original operator-bearing query and verifies supported structured
  constraints again on returned rows.
  Quoted phrases prefer locally stored candidates whose analyzer-normalized words
  are adjacent; they do not exclude other all-term matches.
  An unknown publication date stays empty on every result surface; fetch and
  index time are never presented as publication time.
- **Tavily-compatible `/search`, `/extract`, `/crawl`, and `/map`** with API
  keys and scopes. Raw-content work has fixed concurrency, time, fetch, and
  response-memory budgets, while ordinary search stays on the low-cost path.
  `basic`, `fast`, and `ultra-fast` use local retrieval; `advanced` shares the
  root portal's canonical global ranking for equivalent requests. Default
  results include `raw_content: null`, errors contain only `detail.error`, and
  raw-content requests retain YaGo's stricter 30-second and 200-page safety
  limits.

### 🕷️ A crawler built for the hostile web

- Separate `yago-crawler` worker(s) connected over dedicated control and ingest
  gRPC channels with durable leased orders, nonblocking coalesced progress
  reports, and backpressured at-least-once ingest. A restart on the same durable
  data volume retains committed pages and replays only work whose outcome was
  not committed; at-least-once delivery can repeat an in-flight page. Run-report
  phases are staggered across concurrent crawls; terminal
  snapshots admitted to the bounded queue receive delivery priority, retry, and
  graceful-shutdown drain attempts, while admitted same-ID NAK phases retain
  their ordered lifecycle. Saturation drops a new phase only after expendable
  singleton running state is exhausted and never collapses a protected chain.
  Concurrent document, anchor, URL-metadata, and RWI admission checks share one
  live-capacity observation for at most one second instead of repeating a
  shard-wide disk measurement for every phase; exact metrics and eviction reads
  remain exact and refresh that observation. The node coalesces at most 16 ready
  ingest deliveries for shared vault and Bleve commits, waiting no more than a
  cancel-aware 2 milliseconds for a partial group.
- One live `crawler.max_pages_per_second` ceiling controls page-fetch starts
  across every connected crawler process and active run. It defaults to 10 per
  second; `0` is unlimited. The node leases non-bursting start windows, while
  per-process smoothing, per-run pace, worker concurrency, and per-host
  politeness remain additional limits. A finite ceiling fails closed unless
  both the node and crawler support fetch-start leases, so upgrade them
  together.
- Configuration → Crawler is authoritative for the typed crawler runtime policy;
  environment variables are bootstrap defaults. The policy includes the live
  redirect limit, depth ceiling, host concurrency, crawl delay, fetch timings,
  browser behavior, and shutdown grace. A change that cannot be applied in
  place requests a graceful crawler restart; a browser-sandbox-only change
  retires each Firefox session after its active render. A configured or
  discovered Firefox launcher must resolve through root-owned, non-writable
  path chains to a regular non-set-ID executable available to the crawler; the
  crawler checks this before assembly and again before every spawn. Its optional
  unauthenticated metrics listener accepts loopback IP literals only; remote
  scraping uses a trusted local tunnel or proxy.
- **Atomic node-side crawl control**: `yago-node` keeps its order queue, leases,
  settlements, controls, and terminal-run delivery state in
  `${YAGO_DATA_DIR}/crawlbroker.db`. One bbolt transaction can therefore move a
  lifecycle across all of its indexes. The first enabled crawl-runtime startup
  copies a frozen version-1 state set from the legacy node vault without deleting
  the source, and an interrupted copy resumes before listeners open. The
  dedicated file is outside `YAGO_STORAGE_QUOTA` and main-vault compaction and
  currently has no separate byte cap; `/metrics` exposes its live and allocated
  bytes. A rollback must restore one coordinated stopped node-and-crawler backup,
  because deleting only the dedicated file or downgrading in place can resurrect
  the retained stale cutover state. The sharded collection-length layout is also
  forward-compatible only: after current binaries record new mutations, an older
  binary cannot reconstruct exact lengths from the legacy counter and must not
  open the same data directory.
- **Format coverage beyond HTML**: PDF, DOCX/XLSX/PPTX, legacy DOC/XLS/PPT,
  ODT/ODS/ODP, RTF, EPUB, plain text/CSV, and Markdown (`.md`, `.markdown`,
  `text/markdown`, and `text/x-markdown`) — parsed with stdlib-first
  parsers and validated against real files. PDF extraction follows Page
  `/Contents` and page-reachable Form streams instead of indexing decoded image,
  font, or container payloads. Embedded `/ToUnicode` mappings take precedence;
  a bounded simple-font fallback resolves named, inline, or indirect `/Encoding`
  dictionaries, applies `/BaseEncoding` and single-byte `/Differences`, and maps
  standard glyph names to Unicode. Unknown or untrusted mappings produce no text
  instead of raw glyph-code noise. One document shares a 32 MiB decoded-stream
  budget and a 1 MiB UTF-8 text limit, and no OCR is performed. Synthetic
  regressions mirror malformed and custom-encoding shapes without redistributing
  external PDFs. The live Cisco ENCS document is a verification-only case; its
  previously stored garbage text requires one normal recrawl after upgrade.
- **Two-tier fetching**: fast HTTP first, headless-browser fallback (headless
  Firefox over Marionette, a lazy pool of at most two long-lived processes) for
  browser-resolvable status or bot-wall rejections and successful HTML app shells
  whose executable scripts are paired with insufficient extracted static text.
  Usable static HTML is fetched once, non-HTML never opens a browser, and the
  per-profile browser opt-out remains authoritative. Both paths stay behind the
  dial-time SSRF egress guard.
- **Legacy-web text correctness**: browser-compatible charset decoding handles
  Windows-1251 and other WHATWG encodings, while bounded content-language
  detection keeps documents, facets, RWI postings, and URL metadata aligned.
- **Bounded search memory**: authority, spelling, and optional morphology refresh
  from one completion-relative corpus pass, with fixed-size cross-domain
  citation and frequent-term summaries. The last complete summary is atomically
  stored in the node vault and restored before search listeners open. A fresh
  summary waits only until its original ten-minute due time; a stale summary is
  still served immediately while its replacement scan starts in the background.
  The background pass briefly fences document admission to capture the last key
  of both the legacy and admission-ordered partitions, then reads through those
  boundaries in fixed 16-document keyset pages. Each vault view is released before
  document decoding and analysis, so continuous ingest cannot prolong one pass
  indefinitely and the pass never retains one long Bolt read transaction or claims
  interactive-read priority from ingest writes. Later admissions are included by
  the next pass.
  It also checkpoints and atomically publishes the bounded YaCy host-link graph,
  so peer host-link requests never scan the document vault;
  candidate scans avoid full document bodies; peer and
  web responses, index results, paging sessions, background cache writes, and
  host-link snapshots have process-wide byte or admission limits. `/metrics`
  exposes Go heap plus process RSS for pre-OOM alerts. Interactive searches have
  a hard 1.8-second response deadline and four process-wide outer execution
  slots. Up to 16 admitted HTTP searches wait for an outer slot only inside that
  deadline, and an exact-stage capacity retry may wait for its rescue slot for at
  most 500 milliseconds. Endpoint-owned deadline, capacity, and operational failures are carried
  as partial evidence instead of replacing completed rows with an unavailable
  page; an unexpired successful session may instead be served with the current
  failure evidence, and timed-out work retains its slot until it exits.
  Conflicting vault updates serialize behind writer-only admission while read
  snapshots remain available; served-result denylist checks use an immutable
  in-memory snapshot even after a completed search stage's context ends,
  and the request path waits at most 50 milliseconds for optional click-impression
  preparation while at most four retained impression tasks remain admitted and
  are joined before storage closes. A finished task returns its admission slot
  before its terminal outcome becomes observable, while shutdown still joins
  outcome delivery or abandonment. A click waits for its matching in-flight
  impression commit, and a token whose late commit failed remains rejected until
  it expires.
- Politeness and defense: robots.txt with a standards-compliant 500 KiB parsing
  limit and a sanitizer for real-world malformed files, per-host adaptive pacing
  and crawl delays, URL canonicalization. Discovered links with an unambiguous
  disabled or unsupported file suffix are rejected before frontier admission;
  explicit seeds, extensionless routes, and unknown suffixes still fetch once so
  authoritative response media types can decide. Five consecutive typed availability
  failures retire only that host's remaining URLs in the current run; a success
  resets the evidence, and URL-specific rejections do not penalize a healthy
  host. A single-host run then finishes while a multi-host run continues. The
  Index URL/domain denylist is revisioned to every connected crawler and
  enforced before frontier admission and around each fetch. Further safeguards
  include persistent near-duplicate clustering, crawl-trap defense, per-host and
  per-run page budgets carried by each task profile and editable live in Admin
  Configuration (with a manual per-task override), boilerplate extraction, and a deterministic
  content-quality gate.
- **A living index**: a default 30-day recrawl cadence refreshes pages, and a
  recrawl that finds a page permanently gone (404/410) tombstones it out of
  the index — no eternal dead links. Quota eviction, crawler tombstones, Admin
  deletion, and redirect cleanup share one complete page-lineage owner, so a
  concurrent re-index cannot leave or erase only the document, anchors,
  duplicate cluster, full-text row, postings, or URL metadata.
  On the next reingest or removal, a retained duplicate fingerprint repairs or
  clears a derived cluster row that is missing or no longer lists its URL. The
  bounded repair touches only that URL and its affected clusters, so healthy
  ingest performs no cluster-wide scan and one structural orphan cannot force
  the rest of its ingest group to retry.
  A valid, non-future sitemap `lastmod` can advance the next visit within the
  profile cadence; stale, future, unchanged, or malformed hints cannot create a
  recrawl loop.
- Automatic discovery: enabled swarm greedy-learning uses a depth-5,
  250-page-per-task HTTP-fast-path profile; web-discovery crawling stays opt-in
  with the same ready profile. The same value remains the per-host ceiling, and
  a lower positive global run cap can reduce either automatic task. Explicit
  discovery orders receive fair priority in the durable queue, and every
  profile and document-format control lives in Configuration → Crawler. On
  recovery, a legacy automatic checkpoint that exceeds the visible cap drops
  the newest excess pending pages in bounded, idempotent batches while retaining
  completed totals, visited history, and the oldest pending work.

### 🎨 A public portal your users can keep

- Server-rendered, **works without JavaScript**, accessible (skip links, ARIA,
  keyboard navigation), mobile-friendly, with OpenSearch browser integration
  advertised on every landing and results page so Firefox can offer it as a
  search engine, including when an older saved default theme is active, plus
  RSS/JSON output for every query.
- Every portal and `/yacysearch.html` result carries up to six bounded
  human-readable ranking reasons derived from evidence already computed for that
  request. They do not run a second retrieval, call another provider, or alter
  result order.
- **Operator-themeable end to end**: the search and results pages are
  Handlebars templates editable from the console — visually with a light
  GrapesJS editor that previews the shared portal CSS, or as code with
  CodeMirror — with one-click return to the built-in design and a fallback
  that keeps a broken template from ever blanking the public surface.

### 🛠️ An admin console with everything in one place

- Server-rendered (htmx-enhanced, no SPA, no CDN — every asset self-hosted),
  with one Neutrino-inspired visual system and full no-JS degradation.
- Sections: Overview, Search (with suggestions), Activity, Public portal
  (settings + design tabs), Crawler (dispatch, saved profiles, live monitor,
  per-run detail with up to 64 recent URL outcomes, pause/resume/rate control,
  health), Network (peers, seedlists, news,
  blocking, an explicit public-endpoint self-test, and the complete sortable
  roster paged at exactly 20 peers), Index (browse, bounded per-document stored
  evidence, node/crawler storage-reserve status, delete, blacklist, export,
  schema, and safe next-restart rebuild scheduling), Performance
  (live tiles **and sampled history sparklines**), Backup & restore,
  Configuration (runtime settings with checkboxes, batch save, per-setting
  reset, Crawler/Automatic discovery/Document formats fieldsets, and live
  per-process active-task and fetch-worker limits plus the fleet-wide
  fetch-start ceiling),
  Security (Argon2id admin login, session management, scoped API keys), Logs
  (filterable events with bounded UTC `from`/`to` ranges), Restart (node and
  crawler fleet, can be disabled by config).
- Overview and Index use the authoritative local Bleve document count. Overview
  separately labels YaCy URL metadata records because those populations can
  differ. The Crawler monitor combines every profile in one 20-row-paged view;
  totals and health use the complete snapshot, and each running row keeps its
  controls plus the effective pages-per-minute value together.
- First-run **setup wizard**, CSRF everywhere, strict CSP, login rate
  limiting, and a config-events audit trail. The no-JavaScript login leaves the
  account name empty and shows only bounded public node status; individual
  unavailable system facts degrade independently. An active session receives a
  new unpredictable cookie token after the earlier of one hour or half its
  configured lifetime; rotation preserves the CSRF token and absolute expiry
  and atomically invalidates the old token.

### 📦 Operations without surprises

- One static binary per role; Docker/Compose on the shared `/opt/yago`
  layout, hardened systemd units, and tarballs plus Debian and RPM packages
  built by a tag-driven release pipeline with a verified human-authored
  engineering memo.
- Docker builds pin every builder and runtime base by digest. The node and
  crawler images carry OCI source and revision labels when the caller supplies
  `SOURCE_REVISION`, so two images can be traced to the same source commit.
- Release tags build and smoke-test both product images natively on amd64 and
  arm64, then reject HIGH or CRITICAL findings from the pinned Trivy image
  scan. Their validated configuration and root-filesystem identities are
  promoted without a rebuild into public, provenance-attested
  multi-architecture manifest lists at `ghcr.io/d4rk4/yago-node:vX.Y.Z` and
  `ghcr.io/d4rk4/yago-crawler:vX.Y.Z`. Releases publish no `latest` or shortened
  version alias; deployments can pin the recorded manifest-list digest.
- Prometheus `/metrics` (RED/USE + saturation), burn-rate alert rules with an
  SLO doc, health/readiness endpoints, auth-gated pprof, trace-correlated
  structured logs (never secrets), and a durable event store fed through a
  bounded asynchronous queue. Shutdown drains it for up to five seconds; if a
  writer remains stuck, service shutdown proceeds and storage close waits for
  writer quiescence. HTTP listeners share a fixed fifteen-second shutdown
  budget: ten seconds for graceful requests and five seconds for forced close
  and handler drain. A completed forced close is a clean stop; close or drain
  failures remain actionable errors.
- **Offline backup/restore scripts** for docker and systemd — covered by an
  automated end-to-end round-trip test — plus a console page that shows
  storage usage and hands you the exact commands.
- Storage: a sharded, compressed, quota-bounded vault (bbolt + zstd) where
  losing one shard file loses 1/N of the keyspace, never the store; shard
  integrity checks and index-orphan healing run at startup. Exact collection
  length deltas are recorded on each record's physical shard instead of one
  global writer hotspot, grouped crawler postings commit in retryable chunks of
  at most 8,192, and DHT transfer tallies are coalesced before persistence. If
  normal stale-URL eviction cannot reach the soft quota target, a bounded
  posting scan can reclaim posting-only lineages that have no URL-metadata row
  without selecting metadata-backed URLs through that fallback.
- Outbound traffic is screened in-process at dial time: private networks,
  loopback, link-local, and the cloud metadata range are blocked by default,
  with explicit CIDR allowlists (`YAGO_EGRESS_ALLOW_CIDRS`) when you need
  them.

---

## 🧭 YaCy parity at a glance

The [compatibility matrix](yagonode/doc/compatibility.md) tracks every surface
against upstream YaCy (audited against `yacy/yacy_search_server`):

| Status | Count | Meaning |
| --- | ---: | --- |
| ✅ implemented | 31 | implemented and tested for the documented behavior |
| 🟡 partial | 7 | interoperable core with documented, by-design divergences |
| ⛔ unsupported | 5 | deliberate non-goals (embedded Solr API ×2, Java admin page clones, removed GSA servlet, MCP/OpenAI AI surfaces) |

Highlights: `hello`, `query`, `transferRWI`, `transferURL`, remote `search`,
seed lists, `idx.json`, `list.html`, `message.html`, `profile.html`,
`urls.xml`, `crawlReceipt`, and the `yacysearch.{json,rss,html}` +
`suggest.{json,xml}` + OpenSearch client surfaces all interoperate with Java
peers. The admin plane is deliberately different (Argon2id sessions plus
scoped API keys instead of digest auth) — plain YaCy *search* clients are
unaffected. Remote crawl for the swarm is answered but disabled by default
and can be enabled only on a controlled network using Java-compatible
`salted-magic-sim` authentication, a nonempty shared secret, and exact
trusted-peer and destination allowlists. It delegates bounded URL-only work,
not bodies, profiles, redirects, or follow-up depth, so it remains deliberately
partial rather than full remote-crawler parity
([policy](yagonode/doc/remote-crawl-policy.md)). The public `freeworld` mode
remains the default.

---

## 🚀 Quick start

Requirements: Docker (or Podman); for source builds, Go 1.26.

```sh
export YAGO_SEARCH_API_KEY='replace-with-a-long-random-secret'
cp docker-compose.yml.example docker-compose.yml
make compose-images
docker compose up -d
```

| Port | Listener | Serves |
| --- | --- | --- |
| `8090` | Peer (P2P) | the YaCy wire protocol (`/yacy/*`) — keep reachable |
| `8080` | Public search | portal, `yacysearch.*`, OpenSearch, Tavily `/search`, `/extract`, `/crawl`, `/map` |
| `9090` | Admin & ops | console, `/health`, `/ready`, `/metrics` |

Open `http://localhost:9090/admin/` — the first-run **setup wizard** creates
the administrator account and walks through the initial settings. Enable the
public portal from the Public portal page when you are ready to serve
visitors.

Try it from the command line:

```sh
curl -fsS 'http://127.0.0.1:8080/yacysearch.json?query=test&resource=local'
curl -fsS -H 'content-type: application/json' \
  -H "authorization: Bearer ${YAGO_SEARCH_API_KEY}" \
  -d '{"query":"test","max_results":5}' http://127.0.0.1:8080/search
```

A bare node needs **no configuration** to start: it generates and persists its
peer identity on first run. The variables you are most likely to touch:

| Variable | Default | Meaning |
| --- | --- | --- |
| `YAGO_DATA_DIR` | `/opt/yago/data` (container) | all persistent state — index, vault, identity |
| `YAGO_SEEDLIST_URLS` | _(empty)_ | YaCy seedlists to bootstrap the swarm from; the setup wizard can offer public seeds |
| `YAGO_NETWORK_NAME` | `freeworld` | restart-required YaCy network unit; Configuration → Network & peers persists the operator override |
| `YAGO_PUBLIC_ADDR` | `:8080` | public listener; `off` runs a pure peer node |
| `YAGO_PUBLIC_SEARCH_UI_ENABLED` | `false` | serve the portal at the public root |
| `YAGO_CRAWL_RPC_ADDR` | `127.0.0.1:9091` | crawler integration listener; `off` disables it and `:9091` admits remote workers |
| `YAGO_CRAWLER_MAX_PAGES_PER_SECOND` | `10` | bootstrap in both services for one page-fetch start ceiling across the complete connected crawler fleet; `0` is unlimited, finite operation requires current node and crawler versions, and the Admin value becomes authoritative |
| `YAGO_CRAWLER_MAX_REDIRECTS` | `10` | bootstrap in both services for the live HTTP and browser redirect-hop ceiling; `0` rejects the first redirect, and the Admin value becomes authoritative |
| `YAGO_CRAWLER_BROWSER_PATH` | _(PATH discovery)_ | bootstrap in both services for an optional absolute `firefox` or `firefox-esr` launcher; the persisted Crawler policy is delivered before assembly, and the crawler rechecks its trusted path before every spawn |
| `YAGO_CRAWLER_METRICS_ADDR` | _(disabled)_ | bootstrap in both services for an optional loopback-only crawler metrics listener; the persisted Crawler value is delivered before listener assembly, and remote scraping requires a trusted local tunnel or proxy |
| `YAGO_STORAGE_QUOTA` | `1GB` | soft admission and eviction target for logical live main-vault data; it excludes Bleve, crawl state, allocated free pages, and temporary copies |
| `YAGO_STORAGE_RESERVED_FREE` | `1GB` | filesystem free-space reserve for gate-managed node growth admissions |
| `YAGO_STORAGE_PRESSURE_HYSTERESIS` | `256MB` | additional free space required before gate-managed node growth admissions resume |
| `YAGO_CRAWLER_STORAGE_RESERVED_FREE` | `1GB` | filesystem free-space reserve sent to and bootstrapped by crawler workers |
| `YAGO_CRAWLER_STORAGE_PRESSURE_HYSTERESIS` | `256MB` | additional free space required before crawler frontier growth and fetch admission resume |

The full reference — every variable and its default — is
[doc/configuration.md](yagonode/doc/configuration.md). Bare-metal installs get
hardened systemd units and Debian packages under [deploy/](deploy/).

---

## 🏗️ Architecture

```mermaid
flowchart LR
    subgraph crawler ["yago-crawler (0..n workers)"]
        F["fetch: HTTP + headless browser"] --> P["parse: HTML, PDF, Office, eBooks"]
        P --> I["index artifacts"]
    end
    subgraph node ["yago-node"]
        B[("crawl broker<br/>durable leases")]
        V[("sharded vault<br/>bbolt + zstd")]
        X[("sharded Bleve index")]
        S["search core<br/>RRF + MMR + morphology"]
    end
    I -- "ingest gRPC channel (at-least-once)" --> B --> V --> X --> S
    B <-->|control gRPC: orders · runtime policy · fetch permits · heartbeat · settlement · progress| F
    S --> PEER["peer listener :8090<br/>YaCy RWI/DHT"]
    S --> PUB["public listener :8080<br/>portal · yacysearch · Tavily API"]
    S --> OPS["ops listener :9090<br/>admin console · metrics"]
    PEER <--> SWARM(("YaCy swarm"))
```

| Module | Purpose |
| --- | --- |
| `yagonode` | the peer daemon: protocol endpoints, vaults, search surfaces, portal, admin console, metrics |
| `yago-crawler` | the optional crawler worker: fetch, parse, and emit ingest batches |
| `yagocrawlcontract` | the shared node↔crawler data model and gRPC contract |
| `yagomodel` / `yagoproto` | the YaCy wire model and protocol helpers — reusable on their own |
| `yagoegress` | the shared dial-time SSRF egress guard |

Architecture decisions live in [ADRs](yagonode/doc/adr/README.md), including
the deliberate no-gos; the full feature ledger with
per-feature test pointers is [FEATURES.md](FEATURES.md).

## 🛠️ Development

```sh
make verify   # fmt-check · vet · lint · arch · race tests · exact coverage · build
make e2e      # containerized end-to-end suites (node, crawler, backup/restore)
```

Every feature lands with tests; new third-party dependencies require an ADR
first; `make verify` plus Semgrep and Trivy scans gate every commit. Build and
lint tools are pinned and installed under `.toolchain/` by `make tools`.
Coverage is checked from raw statement totals across all six Go modules; a
self-test proves the checker rejects a profile that display rounding would call
100%. The two isolated testcontainers modules cover the node and crawler. The
crawler suite also exercises the complete YagoRank promotion path with 66
documents across 22 query clusters, split into 1 training, 1 development, and
20 test clusters, and verifies that the promoted model changes the public top
result.

## 📚 Documentation

| Doc | What's inside |
| --- | --- |
| [compatibility.md](yagonode/doc/compatibility.md) | the YaCy parity matrix, endpoint by endpoint |
| [yagorank.md](yagonode/doc/yagorank.md) | the learned ranking stack: model, features, and the tuning loop |
| [configuration.md](yagonode/doc/configuration.md) | every environment variable and its default |
| [specification.md](yagonode/doc/specification.md) | the node's behavior specification |
| [metrics.md](yagonode/doc/metrics.md) · [slo.md](doc/slo.md) | observability and alerting |
| [backup-restore.md](doc/backup-restore.md) | the offline backup/restore procedure |
| [yacy-dht-interop.md](yagonode/doc/yacy-dht-interop.md) | how DHT transfer selection works |
| [remote-crawl-policy.md](yagonode/doc/remote-crawl-policy.md) | default-deny remote crawl, trust, destination, lease, and receipt policy |
| [ADR index](yagonode/doc/adr/README.md) | every architecture decision, including the no-gos |

## 🤝 Credits

This repository started as a fork of
[nikitakarpei/yacy-rwi-node](https://github.com/nikitakarpei/yacy-rwi-node) by
Nikita Karpei, and owes its interoperability to the
[YaCy](https://yacy.net/) project and its two decades of decentralized search.

## 📄 License

[AGPL-3.0](LICENSE). Searches fan out to peers in the YaCy network, which see
your query terms; the portal footer explains result provenance to your users.
