# Bare-metal deployment (systemd + Debian package)

**Quick paths:** `deploy/install.sh` installs from a release tarball;
`deploy/debian/build-deb.sh` builds the `.deb` (the release workflow publishes
both per tag, amd64 + arm64); `deploy/backup.sh` / `deploy/restore.sh` cover
disaster recovery (doc/backup-restore.md).

The same `yago-node` and `yago-crawler` binaries that run under Docker also run
directly on a Linux host, managed by systemd and installed from a Debian
package. This directory holds the reference systemd units and environment files;
`docker-compose.yml.example` is the equivalent reference for the container
deployment, and the two are kept in step.

## Filesystem layout

The Debian package and the systemd units share one layout:

| Path | Contents |
| --- | --- |
| `/opt/yago/bin` | the `yago-node` and `yago-crawler` binaries |
| `/opt/yago/etc` | the environment files below |
| `/opt/yago/data` | durable node state and crawler frontier checkpoints |
| `/opt/yago/data/crawlbroker.db` | atomic node-side crawl queue, lease, settlement, control, and terminal-run state |
| `/opt/yago/data/crawler/frontier-v1.db` | crawler-side frontier, visited set, observations, stable worker identity, and terminal outbox |

The package creates a system `yago` user that owns `/opt/yago/data`. Removing the
package leaves that directory intact. Both systemd services set
`YAGO_DATA_DIR=/opt/yago/data`, so a package upgrade preserves unfinished crawl
work together with the node's broker queue and indexes.

The node creates `crawlbroker.db` only when its crawl runtime is enabled. It is
a dedicated bbolt database, not part of the sharded main vault:
`YAGO_STORAGE_QUOTA`, main-vault eviction, and main-vault compaction do not
limit or shrink it. `YAGO_CRAWLER_NODE_STATE_MAX_BYTES` sets a node-only soft
physical admission boundary, and `YAGO_CRAWLER_FRONTIER_STATE_MAX_BYTES` sets
the crawler checkpoint boundary bootstrapped by both services and delivered in
the live crawler policy. Both default to 4 GiB; `0` disables either boundary.
At or above the node boundary, fresh order enqueue is rejected while migration,
ingest, lifecycle, recovery, and settlement continue. At or above the crawler
boundary, fresh orders wait before expansion and newly discovered links are
refused, while an already admitted seed manifest, existing dispatch, recovery,
lifecycle, and terminal settlement continue. Raising or disabling the crawler
boundary wakes waiting orders. Monitor
`crawl_broker_state_used_bytes` for live database use and
`crawl_broker_state_file_bytes` for the allocated file size; bbolt can retain
free pages, so the latter is the disk-capacity signal.

On startup, a state file whose physical size is at least its enabled boundary
is copied into a private sibling bbolt file and atomically replaced after the
copy is synced. This can require temporary free space. A failure before
replacement leaves the original authoritative; startup logs a warning and
continues with it. A failure while syncing the containing directory after
replacement is logged as an installed durability warning. These are soft
admission boundaries, not filesystem quotas.

`YAGO_STORAGE_RESERVED_FREE` and `YAGO_STORAGE_PRESSURE_HYSTERESIS` control the
node's filesystem-pressure admission gate. The corresponding
`YAGO_CRAWLER_STORAGE_RESERVED_FREE` and
`YAGO_CRAWLER_STORAGE_PRESSURE_HYSTERESIS` values control each crawler data
volume and are also sent by the node after a current crawler heartbeat. The
defaults are 1 GB reserved and 256 MB recovery hysteresis. These controls pause
gate-managed crawl and index ingestion; they are not a filesystem quota.
Deleting bbolt rows can leave reusable pages inside `crawlbroker.db` or
`frontier-v1.db` without increasing operating-system free space. If pressure
persists, free filesystem space or lower the applicable reserve and hysteresis.
Use a filesystem/project quota or a quota-capable volume for a hard aggregate
boundary.

The crawler creates throwaway Firefox profiles in its private process temporary
directory. They are not durable state. Its frontier checkpoint is durable and
lives under `YAGO_DATA_DIR`. `YAGO_CRAWLER_MAX_PAGES_PER_RUN` bootstraps a
50,000-page whole-run budget in both services; the node records the current
Admin Configuration value in each new crawl profile, while the crawler value
remains the fallback for queued legacy profiles. `0` removes this general bound
for manual profiles, but it does not remove the dedicated swarm or web-discovery
task cap. A legacy automatic checkpoint derives that cap from its positive
per-host ceiling and trims excess pending work idempotently during recovery.

The container images use different unprivileged identities: UID 65532 for the
node and UID 65534 for the crawler. Docker Compose therefore mounts separate
`yago-data` and `yago-crawler-data` named volumes at `/opt/yago/data` inside the
respective containers. Do not mount one writable volume into both containers.

## Package dependencies

The binaries rely on facilities the operating system provides, installed as
package dependencies rather than bundled:

- `ca-certificates` — trust roots for outbound TLS. The binaries read the system
  trust store; there is no baked-in certificate bundle on bare metal.
- `firefox-esr` on Debian (or `firefox` on Ubuntu/Fedora) — the crawler's
  slow-path browser, driven headless over Marionette. The container image
  bundles `firefox-esr`; a host install discovers `firefox-esr` and then `firefox`
  on `PATH`, while `YAGO_CRAWLER_BROWSER_PATH` can pin an absolute non-standard
  location whose exact basename is `firefox-esr` or `firefox`. Keep the same
  bootstrap in the node and crawler environments; Configuration → Crawler
  persists and delivers the authoritative value. Before browser assembly and
  again immediately before each spawn, the crawler requires every resolved
  executable-chain component to be root-owned and not group- or other-writable,
  and requires the final regular file to be executable by the crawler identity
  without set-user-ID or set-group-ID bits. An empty setting with no discoverable
  Firefox remains nonfatal: the crawler continues on the HTTP fast path and
  reports browser fallback unavailable only when that slow path is needed.

## Install and run

`deploy/install.sh` performs the whole layout: it creates the `yago` system
user, the `/opt/yago/{bin,etc,data}` tree with correct ownership, copies the
binaries, environment examples, and CJK dictionary notices (never overwriting
an edited environment), installs the units, and enables them when systemd is
running. It is idempotent:

```sh
sudo deploy/install.sh <dir-with-binaries>
```

Or by hand:

```sh
sudo cp yago-node yago-crawler /opt/yago/bin/
sudo cp deploy/systemd/yago-node.service deploy/systemd/yago-crawler.service /etc/systemd/system/
sudo cp deploy/systemd/yago-node.env.example /opt/yago/etc/yago-node.env
sudo cp deploy/systemd/yago-crawler.env.example /opt/yago/etc/yago-crawler.env
# edit the two env files, then:
sudo systemctl daemon-reload
sudo systemctl enable --now yago-node
sudo systemctl enable --now yago-crawler   # optional crawler worker
```

## Coordinated node-crawler upgrades

The node and crawler control protocol requires the worker, process session, and
active lease identities introduced with persistent frontier recovery. A current
node rejects an older crawler that omits its session identity, and a current
crawler cannot safely infer lease ownership from an older heartbeat response.
Recovered-session manifests, explicit delivery-confirmation presence, and the
live active-task directive are additive for decoder compatibility, but they do
not make mixed-version operation a supported deployment.
Do not run mixed versions. Stop the crawler before the node, replace both
binaries or install the matched package, start the node, wait for readiness, and
then start the crawler. The stable crawler identity and frontier checkpoint must
remain on the same `YAGO_DATA_DIR` volume.

The v0.0.12 crawler runtime rename is an installation boundary. Take a generic
backup with the tooling from the currently installed release, then leave both
services stopped for package installation. The pre-install migration removes
the superseded unit registration and executable, rewrites the former crawler
environment variable prefix into `YAGO_CRAWLER_*`, and preserves the edited
values in `/opt/yago/etc/yago-crawler.env`. If that canonical file already
exists, it remains authoritative. Start only `yago-node.service` and
`yago-crawler.service` after installation; there is no compatibility alias.

On the first current startup with crawling enabled, the node opens
`${YAGO_DATA_DIR}/crawlbroker.db` and copies one frozen version-1 set of broker
and terminal-run buckets from the legacy main vault before opening listeners.
The copy commits resumable 256-row pages, verifies source and target, and leaves
the legacy rows unchanged. A conflict or verification failure stops startup.
The file lock wait is bounded to five seconds; a timeout normally means another
node still has the same data directory open. Do not start a second node against
that directory.

After the migration marker commits, the dedicated file is authoritative and the
retained legacy rows are only a stale cutover copy. They continue to occupy main
vault quota but are not kept in sync. Do not delete only `crawlbroker.db` to
retry an upgrade, and do not install an older node over the upgraded data tree:
either path can import or operate on the stale copy and resurrect already
settled work. The migration also cannot repair a cross-bucket transition that
was already inconsistent in the historical sharded state. Rollback means stop
the crawler and node, restore their complete matching pre-upgrade data, install
the matching older binaries, start the node, verify readiness, and then start
the crawler.

For an ordinary restart, graceful node shutdown closes the broker before the
dedicated file. A crash restart sees only committed bbolt transactions. A
crawler reconnecting with the stable identity from the same data volume adopts
its session-aware leases; deferred and legacy sessionless leases retain their
expiry-and-requeue behavior. Its separate frontier does not fetch a page whose
outcome committed before the stop, while an unfinished outcome remains eligible
for replay. Recovery declares the complete adopted-lease manifest once, confirms
ordered batches of at most 16, and does not begin ordinary delivery until the
manifest is complete. Periodic full-set heartbeats keep leases alive without
confirming an unseen batch.

## Secure YaCy remote crawl delegation

Remote crawl delegation is disabled by default on Docker, systemd, and package
installs. Enabling it requires one complete controlled-network policy:

- `YAGO_NETWORK_AUTHENTICATION=salted-magic-sim` and a nonempty
  `YAGO_NETWORK_AUTHENTICATION_SECRET`;
- `YAGO_REMOTE_CRAWL_TRUSTED_PEERS` with 1–256 exact 12-character peer hashes;
- `YAGO_REMOTE_CRAWL_ALLOWED_DESTINATIONS` with 1–256 exact domains or IP prefixes,
  excluding address-family wildcard prefixes;
- `YAGO_REMOTE_CRAWL_ENABLED=true`.

The remaining bootstrap controls are
`YAGO_REMOTE_CRAWL_REQUESTS_PER_MINUTE=60`,
`YAGO_REMOTE_CRAWL_OUTSTANDING_PER_PEER=10`,
`YAGO_REMOTE_CRAWL_LEASE_TTL=10m`, and
`YAGO_REMOTE_CRAWL_QUEUE_CAPACITY=1000`. Configuration → Swarm exposes the same
seven remote-crawl values, stores overrides in the node vault, validates the
complete policy before accepting an enable or security downgrade, and applies
them after the next restart. The environment is only the bootstrap default.

The node copies eligible URLs from locally accepted crawl orders into a separate
durable delegation queue without delaying or removing the local order. This
preserves local work when DNS, storage, peer, or queue admission fails. It also
means a local crawler and a trusted peer may fetch the same URL; normal URL and
content reconciliation handles that intentional duplication.

Delegation is limited to one URL per item and never grants a crawl profile or
follow-up depth. Feed requests return at most 100 URLs inside a 1–20 second
request budget. Only HTTP and HTTPS default ports are accepted. Exact destination
policy and every DNS answer are revalidated when work is staged, leased, and
receipted; loopback, link-local, multicast, unspecified, metadata, and other
reserved destinations remain denied. Receipt fields, decoded metadata, and URL
length are bounded, and only an unexpired peer-and-URL-matching `fill` receipt can
commit metadata. Other valid YaCy outcomes requeue the URL, and a receipt cannot
create or extend work.

Queue, lease, and per-peer request-rate state survive node restart. Every
decision is reported through the `remote_crawl_decisions_total` metric and
stable structured logs. Only warning and security outcomes enter the durable
Admin event history, so normal accepted traffic cannot evict actionable events.
The seed remote-crawl capability flag is advertised only while the complete
policy is active.

## Backup and restore

A consistent backup must quiesce the crawler frontier, the node's dedicated
`crawlbroker.db`, and the main node vault at the same time. `deploy/backup.sh`
records which services are running, stops only those services with the crawler
before the node, and restarts only that original set with the node before the
crawler. Docker mode archives both named volumes into one file; systemd mode
archives their shared `/opt/yago/data` directory. A copy or required restart
failure makes backup fail visibly, and the crawler is not started after a node
restart failure. Restore follows the same state-preserving stop/start order and
resets each Docker volume to its image UID. Before stopping a service, restore
validates and stages the complete archive, rejects links, special files, unsafe
paths, and an unexpected flat/dual-volume layout. Replacement is
rollback-protected. Systemd restore rejects relative, missing, root, and
top-level operating-system data paths and preserves the target directory's
owner and mode.

```sh
deploy/backup.sh docker docker-compose.yml \
  yago-node yago_yago-data /srv/backups \
  yago-crawler yago_yago-crawler-data
deploy/backup.sh systemd \
  yago-node.service /opt/yago/data /srv/backups yago-crawler.service
```

Compose volume names include the project prefix by default; confirm them with
`docker volume ls`. The complete restore commands and historical flat-archive
compatibility are documented in `doc/backup-restore.md`.

Restore the complete node tree and crawler checkpoint from the same archive.
Restoring only the main vault, only `crawlbroker.db`, or only the crawler volume
can combine a pending order, lease, settlement, and page frontier from different
moments. Start the restored node first and verify `/ready`; only then start the
crawler so it can adopt its retained session-aware leases against the matching
broker state.

## The browser sandbox on bare metal

Headless Firefox has its own content-process sandbox. It needs unprivileged user
namespaces, which the container image and most current Linux hosts (Ubuntu
23.10+, AppArmor userns restrictions) do not grant, so the crawler defaults to
`YAGO_CRAWLER_BROWSER_SANDBOX=false` in both service environments and launches
Firefox with the content sandbox disabled (`MOZ_DISABLE_CONTENT_SANDBOX=1`). On bare metal the systemd unit is the
isolation boundary — it runs the crawler as an unprivileged user with
`NoNewPrivileges`, a private `/tmp`, and a read-only system — and the crawler is
already egress-guarded against private networks.

An operator on a host that supports the browser sandbox can opt back in with the
persisted Configuration → Crawler “Firefox content sandbox” setting, or by
setting `YAGO_CRAWLER_BROWSER_SANDBOX=true` in both service environments before
the first start, **and** relaxing the unit (`NoNewPrivileges=no`, and allow user
namespaces); Firefox cannot start its content sandbox under `NoNewPrivileges`.
A live change lets an active render finish, retires each pooled Firefox session,
and uses the new value before that slot's next render. An immediately older
policy-capable node omits the optional field, so the crawler retains its own
bootstrap or already effective value instead of treating omission as `false`.

## Crawler resource limits

`YAGO_CRAWLER_WORKERS` is a bootstrap value in both service environment files
and must be kept equal. It is the exact number of page-fetch workers in each
connected `yago-crawler` process, not a crawl-run or task limit, and accepts
values from 1 through 256. After a crawler's first heartbeat, the persisted
Configuration → Crawler value on `yago-node` becomes authoritative and is sent
to every connected crawler. A live resize stops new page intake, lets the
current in-flight fetches finish, and then starts the requested worker group;
the crawler process does not restart. With several crawler processes the value
applies independently to each process, so aggregate fetch concurrency is the
per-process setting multiplied by the number of connected processes.

`YAGO_CRAWLER_MAX_PAGES_PER_SECOND` is a bootstrap value in both service
environment files and must be kept equal. It defaults to 10 and accepts
0–1,000,000, where zero is unlimited. The persisted
`crawler.max_pages_per_second` value from Configuration → Crawler becomes
authoritative after heartbeat and changes live. The node leases strict relative
start windows from one non-bursting fleet schedule shared by every connected
crawler process and active task. A crawler measures each completed lease RPC,
caps its delivery observation at one second, and reports that allowance on the
next sequence. The node widens that demand-backed batch by the reported prior
delivery time while reserving its complete final window before another batch.
The crawler intersects relative openings with response receipt and closings with
request send, then preserves the configured interval between permits it actually
uses. A latency spike beyond the preceding allowance can discard a permit but
cannot produce a catch-up burst; the new measurement applies to the next
sequence. Each crawler retains the same value as a local process smoother;
per-run pace, per-host concurrency, robots crawl delay, and the per-process
worker limit remain additional bounds. Zero disables both rate limits. A finite
rate requires node and crawler binaries that support fetch-start leases; an older
peer fails closed instead of bypassing the fleet ceiling.

`YAGO_CRAWLER_MAX_REDIRECTS` is a bootstrap value in both service environment
files and must be kept equal. It defaults to 10 and accepts 0–1,000, where zero
rejects the first redirect. The persisted `crawler.max_redirects` value applies
live to guarded HTTP clients. Existing Firefox sessions close and lazily
relaunch with the new browser redirect limit before the next render.

`YAGO_CRAWLER_MAX_ACTIVE_RUNS` is a separate bootstrap value in both service
environment files and must also be kept equal. It defaults to 32 and accepts
1–256. After heartbeat, the persisted `crawler.max_active_runs` value from
Configuration → Crawler is authoritative for every connected process. A slot is
held from prepared-order admission through terminal completion, so excess
ordinary and recovered tasks wait without activating another frontier or
periodic progress reporter. Increasing the value wakes waiting work. Decreasing
it does not cancel active tasks; replacements wait until occupancy falls below
the new limit. The limit is per process, independent of page-fetch workers, and
does not impose a fleet-wide task ceiling. The separate fetch-start lease
schedule enforces the fleet-wide pages-per-second ceiling.

The crawler private-network policy, Firefox executable and content sandbox,
browser failure threshold, loopback metrics listener, HTTP phase and
whole-request timeouts, default crawl delay, maximum depth and per-host
concurrency, per-run default rate, sitemap limit, shutdown grace, and HTTP
User-Agent are also bootstrapped in both service environments. The node's
persisted Configuration → Crawler values are authoritative. Each crawler reads
the typed policy before assembling its fetch stack. A sandbox-only change
relaunches pooled browsers lazily before their next render, and a frontier-state
boundary change updates admission and wakes waiters live; another changed
policy received on a later heartbeat triggers a graceful automatic crawler
restart. The sandbox, browser-path, metrics-address, and frontier-state maximum
fields are optional on the wire; their absence preserves the crawler's current
values when an immediately older policy-capable node omits them. A current node
sends all four fields explicitly and is authoritative. An older node without
the additive policy RPC leaves the crawler environment values in effect.
Private CIDR exceptions accept only RFC 1918 and IPv6 ULA subnets and never
admit loopback, link-local, metadata, carrier-grade NAT, multicast, or reserved
ranges.

Both service environment files bootstrap
`YAGO_CRAWLER_PRIORITIZE_AUTOMATIC_DISCOVERY=true` and must be kept equal.
When enabled, explicit swarm and web-discovery work receives at most three
durable order leases and three due page dispatches before waiting normal work.
Disabling it restores global FIFO across the durable priority classes and the crawler's
existing run-fair, value-scored page selection across both classes. The first
successful crawler heartbeat is authoritative before order intake. If that
one-second startup attempt fails, the crawler uses its environment bootstrap until
a periodic heartbeat succeeds. The Admin setting changes both live without
moving, rewriting, or dropping queued orders.

Pending order payloads remain in the established `crawlorders` bucket. The
priority indexes are additive and contain only keys, so an older package ignores
priority but continues to drain the complete queue in global FIFO order. After
returning to the current package, startup indexes the orders admitted by the
older node and selection removes stale keys for orders consumed while downgraded.
An unsettled lease created while downgraded recovers its class from the retained
order payload. This storage compatibility does not make a live mixed-version
node/crawler pair supported; use the coordinated upgrade sequence above.

`yago-crawler.service` applies cgroup controls to bound headless-Firefox memory
and task growth while giving the co-located node greater relative CPU weight:
`MemoryHigh=60%` applies reclaim pressure (it throttles, never kills), and
`MemoryMax=85%` confines its out-of-memory selection to the crawler cgroup. A
killed Firefox child is replaced by its session manager; repeated launch or
render failures cool only that session. If the crawler process is killed,
`Restart=on-failure` restarts the service. `TasksMax=4096` bounds thread/process
explosion, and `CPUWeight=50` weights the node more heavily under contention.
The process uses the moderate `Nice=5` and
best-effort I/O priority 6, with `IOWeight=50`, so it still yields to the node
without the indexing starvation caused by the previous lowest-possible CPU and
idle-I/O policy. `CPUWeight` remains the primary cross-cgroup CPU lever and can
be lowered further to give the node a larger share. The percentages are relative
to physical RAM, so they scale to any box. Tune them per host with a drop-in rather than editing the
shipped unit:

```
systemctl edit yago-crawler
# [Service]
# MemoryHigh=2G
# MemoryMax=3G
```

A small single-purpose box can raise the shares; a box that co-hosts other
services can lower them or switch to absolute sizes as above.

When the loopback crawler metrics listener is enabled, use
`yacy_crawler_browser_slot_acquisition_seconds` to measure pool queueing,
`yacy_crawler_browser_sessions{state="ready|busy|cooling"}` to see the fixed pool
state, and
`yacy_crawler_browser_failures_total{reason="slot_deadline|cooldown|launch|render"}`
to classify bounded failure causes. The labels are fixed and never contain a URL
or raw error text. The existing
`yacy_crawler_browser_slot_acquisition_deadlines_total` remains available for
dashboards that predate the reason-labeled counter.

## Node resource limits

`yago-node.service` applies `MemoryHigh=75%`, `MemoryMax=90%`, and
`TasksMax=8192`. The soft boundary gives the kernel room to reclaim mapped index
pages before the host is exhausted; the hard boundary confines a future runaway
allocation to the node cgroup, where `Restart=on-failure` can recover it instead
of invoking the host-wide OOM killer. These limits are a final containment layer,
not a memory-management strategy: corpus-derived vocabularies, search pages,
background writes, and compatibility graphs remain bounded in the process.

Monitor `go_memstats_heap_alloc_bytes` and `process_resident_memory_bytes` after
an upgrade, then use an absolute-size drop-in on a co-hosted machine when its
steady-state working set is known:

```
systemctl edit yago-node
# [Service]
# MemoryHigh=6G
# MemoryMax=8G
```

Keep enough space above the measured live heap for garbage collection and above
the resident working set for the mapped vault and Bleve shards. A limit below
the live set causes reclaim or GC churn and cannot repair an unbounded data
structure.

## Full-text schema rebuilds

An upgrade that changes the embedded Bleve mapping rebuilds all eight search
shards from the document vault before the public and peer listeners start. The
node records the rebuild as in progress and repeats it from the beginning after
an interrupted attempt, so a partial index is never served. Before removing the
old index, startup counts the stored documents, measures the current index
footprint when available, and applies the normal storage-growth admission with
that measured footprint as additional headroom. A failed preflight keeps the old
index and durable rebuild marker intact and stops startup with an actionable
error. The footprint is an estimate of rebuild demand, not an exact upper bound:
segment allocation and merging can still raise the transient peak after the
check.

An admitted rebuild writes one structured INFO start record with the document
total, footprint estimate, and storage observation, then at most one INFO record
at each 10% milestone from 10% through 90%, and one completion record with the
indexed total, batch total, and elapsed milliseconds. Rebuild writes use
16-document batches to limit transient segment memory, but startup downtime,
merge I/O, and temporary disk use still scale with the stored corpus. An
interrupted rebuild still restarts from the beginning; the progress records are
operational evidence, not a resumable checkpoint or Admin progress control.

For a bare-metal package upgrade, use the stopped two-service backup so the
crawler frontier and node broker queue share one recovery point, install the
published package, and start `yago-node` first while watching its journal, RSS,
and free disk space. Start `yago-crawler` only after the node is ready. Do not use
the normal restart window for a mapping-changing release until the same corpus
size has been timed on representative storage and the maintenance window covers
the measured rebuild.

Main-vault startup reports each sequential shard open and the subsequent RWI
word-filter initialization as stable JSON records on standard output, which the
service journal captures. Successful
completion is INFO; a degraded filter completion is WARN and includes its shard
total. A binary that
supports clean-shutdown freelist checkpoints must complete one orderly shutdown
before later planned restarts can load those checkpoints directly. Its first
start after upgrading from an older binary can therefore still perform the full
scan, as does every start after an unclean stop. The in-memory word filters are
rebuilt on every start even when the freelists load directly. The shipped
Compose and systemd definitions allow up to 15 minutes for the node to finish
draining event persistence and checkpointing its shards; ordinary exits return
sooner. Event persistence gets five seconds to drain and five more seconds to
quiesce after cancellation. If a writer remains active, the node reports an
error and skips vault close rather than racing that transaction; the following
start performs the ordinary recovery scan. Do not lower the supervisor limit
below the measured clean-shutdown time for the deployed vault and storage.

The append-ordered document layout is a forward-compatible upgrade, not an
in-place downgrade format. An older binary ignores documents admitted into the
ordered partition and can create conflicting legacy rows. Rollback therefore
requires stopping both services, restoring the stopped pre-upgrade data backup,
installing the older package, and then starting the node before the crawler.
Never run an older package against a data directory already opened by this layout.

## Debian package

`deploy/debian/build-deb.sh <version> <arch> <bindir> <outdir>` builds the
package anywhere `dpkg-deb` exists. It installs into `/opt/yago`, depends on
`ca-certificates` and `adduser` (recommends `firefox-esr | firefox`, so the
crawler's browser resolves on both Debian and Ubuntu), creates the `yago`
system user in postinst, enables the systemd units, and — validated end to end
on Debian 12 and 13 and Ubuntu 24.04 and the latest LTS — keeps `/opt/yago/data`
through purge while removing the edited env files. The binaries are static (CGO
off), so the same package installs across every 24.04-and-newer release (and the
short-lived interim releases in between) with no glibc coupling.

Download and verify a published package before installation. The attestation
binds its digest to the tagged source and release workflow in `D4rk4/yago`; it
does not replace the backup and post-install health checks:

```sh
version=vX.Y.Z
gh release download "$version" --repo D4rk4/yago \
  --pattern "yago_${version#v}_amd64.deb" --dir /tmp/yago-release
source_digest=$(gh api "repos/D4rk4/yago/commits/$version" --jq .sha)
gh attestation verify "/tmp/yago-release/yago_${version#v}_amd64.deb" \
  --repo D4rk4/yago \
  --signer-workflow D4rk4/yago/.github/workflows/release.yml \
  --source-ref "refs/tags/$version" \
  --source-digest "$source_digest"
```

The release gate compares the notice bytes inside the Debian archive with the
pinned source before distribution smoke tests. Minimal container images may set
a dpkg `path-exclude` for `/usr/share/doc/*`; their smoke test validates the
package manifest instead of requiring dpkg to materialize a file that local
policy deliberately excludes. Traced package-smoke shells identify the exact
failed assertion.

### `yagoseek.dev` production exception

The operator policy for deployments performed as `root@yagoseek.dev` skips a
new pre-upgrade backup to keep the outage bounded. Retain every existing archive
under `/opt/yago/backups` unchanged; do not delete, replace, prune, or reuse one
as staging space. Download the release package under
`/opt/yago/releases/vX.Y.Z`, verify its attestation and package identity there,
stop `yago-crawler` and `yago-node`, install the package, then start and verify the
node before starting the crawler. This target-specific exception accepts less
rollback coverage and does not change the generic backup requirement for any
other host.

## RPM package

`deploy/rpm/build-rpm.sh <version> <arch> <bindir> <outdir>` builds the same
layout as an `.rpm` anywhere `rpmbuild` exists (the `<arch>` is the Go/deb
`amd64|arm64`, mapped to the RPM `x86_64|aarch64`, so one release matrix drives
both package builders). It requires `ca-certificates`, recommends `firefox`,
creates the `yago` system user in a `%pre` scriptlet, enables the units in
`%post`, and keeps `/opt/yago/data` on removal. The static binaries carry no
shared-library dependencies, so debuginfo and strip post-processing are disabled
(they would run the host toolchain against a cross-arch binary) and the same
package installs across Fedora, RHEL/Rocky/Alma 9+, and openSUSE. Validated end
to end on Fedora and Rocky 9.

## Releases

Pushing a `v*` tag whose commit belongs to `main` runs
`.github/workflows/release.yml`: `make verify` gates the release, binaries build
for amd64 and arm64 (CGO off, trimmed) with the tag
stamped in as the canonical `vN.N.N` product version (`yago-node --version` /
`yago-crawler --version` report it), each arch ships as a tarball (binaries + install.sh +
units + backup doc + CJK dictionary notices), a `.deb`, and an `.rpm`. Debian,
RPM, and node-container distributions install the same notices under
`/usr/share/doc/yago`. The amd64 `.deb` is smoke-installed
across Debian 12/13 and Ubuntu 24.04 + `ubuntu:latest`, and the amd64 `.rpm`
across Fedora and Rocky 9 — each run checks the declared dependencies resolve,
both binaries report the stamped version, and package removal keeps
`/opt/yago/data`. The package smoke matrix also exercises both an environment
migration and an interrupted migration where the canonical file wins. Every
tarball, Debian package, and RPM package receives
Sigstore-signed GitHub provenance after package construction and the applicable
amd64 smoke tests; the release job verifies
the downloaded artifact attestations before publication. Before tagging, commit the human-authored engineering memo as
`doc/releases/vX.Y.Z.md`. The workflow verifies its Abstract length, read-more
delimiter, and stable section order, then uses that exact memo for the GitHub
Release; a missing or malformed memo stops publication. A blocking native
Linux amd64/arm64 matrix builds both product containers with
`VERSION=$GITHUB_REF_NAME` and `SOURCE_REVISION=$GITHUB_SHA`, verifies the
Docker-reported architecture, OCI source and revision labels, exact binary
versions, and bundled Firefox executable, then scans both images with Trivy
0.72.0 for HIGH or CRITICAL vulnerabilities, secrets, and misconfigurations.
Each native job exports a checksum-protected archive of its validated images as
a short-lived workflow artifact. After every matrix member succeeds, a separate job verifies
and reloads those archives, rechecks their release identity, publishes the two
platform manifests, and creates one multi-architecture GHCR manifest list per
product. It attaches and verifies GitHub-hosted provenance for each final
manifest-list digest before the GitHub Release is published. Container packages remain
separate from the six GitHub Release file assets.

The complete `vX.Y.Z` tag is the only operator-facing image reference;
immutable `vX.Y.Z-amd64` and `vX.Y.Z-arm64` staging references exist only to
compose the manifest list. CI does not create `latest`, major-only, minor-only, branch,
or date aliases. The one-time v0.0.10 backfill used a temporary
`workflow_dispatch` run from `main`. It was fixed to the exact release ID, tag
ref, tag object, source commit, successful validation jobs, and image artifact
digests. Package construction and GitHub Release creation remained disabled;
the path could not move the tag, replace an existing manifest, rebuild package
assets, or recreate the release. Standard GitHub Actions provenance recorded
the repair invocation, while a separate signed identity predicate recorded the
historical release and validation. The temporary trigger was removed after
public pulls and both evidence types passed. Build development images locally
with `make compose-images`.

## Published container images

Every container release publishes these public manifest lists for Linux amd64
and arm64:

- `ghcr.io/d4rk4/yago-node:vX.Y.Z`;
- `ghcr.io/d4rk4/yago-crawler:vX.Y.Z`.

GitHub documents both a private default for first personal-account packages and
repository-visibility inheritance for packages created by a `GITHUB_TOKEN`
workflow. Observed anonymous registry access is authoritative. Before publishing
a new package name, configure the
`release-container-public-visibility` environment with an owner as a required
reviewer and allow self-review. The authenticated publication job creates and
attests the exact manifest; the next job remains pending at that protected
environment. Test the exact package tag without registry credentials. If it
remains private, the owner changes it to Public through Package settings;
otherwise no visibility change is required. Approve the pending deployment only
after anonymous access is observed. Public visibility is irreversible. The
package REST and GraphQL APIs expose no supported visibility mutation.

The approved job uses an empty Docker credential directory to pull both the
exact tag and its digest before it can succeed. Remove the environment's
one-time required-reviewer rule only after that anonymous gate proves the new
package public. Existing public package names need no visibility change.
A retry accepts an existing architecture tag or manifest list only when its
image identity, labels, platforms, and child digests match the validated
archives; registry authorization, network, and server failures stop publication
rather than being interpreted as a missing tag.

Create the one-time environment protection before pushing the tag that first
uses a package name:

```sh
reviewer_id=$(gh api user --jq .id)
jq -n --argjson reviewer_id "$reviewer_id" '{
  wait_timer: 0,
  prevent_self_review: false,
  reviewers: [{type: "User", id: $reviewer_id}],
  deployment_branch_policy: null
}' | gh api --method PUT \
  repos/D4rk4/yago/environments/release-container-public-visibility \
  --input -
```

After authenticated publication finishes, test the exact tag with an empty
Docker credential directory. Change the package to Public at the settings URL
shown by the pending environment only if that test reports it private. Approve
the pending deployment only after the anonymous tag resolves. Querying
`repos/D4rk4/yago/actions/runs/RUN_ID/pending_deployments` returns the environment
ID for an API approval when desired. After the workflow independently verifies
the tag and digest, update the same environment with an empty `reviewers` array;
later releases retain the deployment record without waiting for a redundant
public-state action.

After that gate passes, select the exact version and verify the embedded product
identity before use:

```sh
version=vX.Y.Z
node_image=ghcr.io/d4rk4/yago-node
crawler_image=ghcr.io/d4rk4/yago-crawler
docker pull "$node_image:$version"
docker pull "$crawler_image:$version"
test "$(docker run --rm "$node_image:$version" --version)" = "yago-node $version"
test "$(docker run --rm "$crawler_image:$version" --version)" = "yago-crawler $version"
```

The release memo records each immutable image tag. The completed workflow output
and GHCR package page record each multi-architecture manifest-list digest. Pin
that digest for a deployment so a tag lookup is not part of future selection,
then verify its GitHub-hosted provenance:

```sh
version=vX.Y.Z
node_image=ghcr.io/d4rk4/yago-node
crawler_image=ghcr.io/d4rk4/yago-crawler
source_digest=$(gh api "repos/D4rk4/yago/commits/$version" --jq .sha)
workflow_digest=$source_digest
node_digest=sha256:replace-with-the-recorded-node-manifest-digest
crawler_digest=sha256:replace-with-the-recorded-crawler-manifest-digest
docker pull "$node_image@$node_digest"
docker pull "$crawler_image@$crawler_digest"
gh attestation verify "oci://$node_image@$node_digest" \
  --bundle-from-oci \
  --repo D4rk4/yago \
  --signer-workflow D4rk4/yago/.github/workflows/release.yml \
  --signer-digest "$workflow_digest" \
  --source-ref "refs/tags/$version" \
  --source-digest "$source_digest" \
  --deny-self-hosted-runners
gh attestation verify "oci://$crawler_image@$crawler_digest" \
  --bundle-from-oci \
  --repo D4rk4/yago \
  --signer-workflow D4rk4/yago/.github/workflows/release.yml \
  --signer-digest "$workflow_digest" \
  --source-ref "refs/tags/$version" \
  --source-digest "$source_digest" \
  --deny-self-hosted-runners
```

Constrain normal release verification to `refs/tags/$version` and the commit
resolved from that tag, as shown in the Debian package example. A historical
backfill is different: its certificate truthfully names the current
`refs/heads/main` workflow invocation and workflow-definition commit. Verify
those certificate fields for both attestations. Require the standard SLSA v1
predicate to use `https://actions.github.io/buildtypes/workflow/v1` and match
the subject, current workflow, builder, and invocation. Separately require the
project-specific historical-release identity predicate to match release ID
355175485, tag `v0.0.10`, tag object
`09ca7be1b1e5065155111479c9213bd0566801d8`, source commit
`9bcc0bde61364c8248fba7f452c19f2446c72898`, and the validation run recorded in
the release's dated factual correction. An attestation establishes provenance,
not image safety, and does not replace the native smoke tests, Trivy policy, or
runtime health checks.

## Container build provenance

Both product Dockerfiles pin the Go builder and final runtime base images by
SHA-256 digest. Base-image changes are therefore explicit source changes rather
than mutable-tag resolution at build time. The readable tags remain beside the
digests so operators can see the selected release.

Release CI supplies `VERSION` and `SOURCE_REVISION` from the exact tag and
source commit. Set `SOURCE_REVISION` explicitly when building another source
checkout. Compose passes it to both product images and each final image records it in the
`org.opencontainers.image.revision` label; the source repository is recorded in
`org.opencontainers.image.source`.

```sh
SOURCE_REVISION=$(git rev-parse HEAD) make compose-images
docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }} {{ index .Config.Labels "org.opencontainers.image.source" }}' yago-node:latest
docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }} {{ index .Config.Labels "org.opencontainers.image.source" }}' yago-crawler:latest
```

The default revision is `unknown`, which makes an omitted caller stamp visible
instead of guessing from a possibly dirty worktree. `SOURCE_REVISION` identifies
source provenance. `VERSION` is the product version carried by both binaries.
The image Make targets use an exact `vN.N.N` tag at `HEAD` only when the
checkout has no tracked, staged, or untracked change; every other checkout uses
the current UTC build date as `YYYY.MM.DD-dev`. Deriving the
date outside the Dockerfile makes it part of the cache key, so a later day's
build cannot reuse an older dated binary layer. Build both Compose images with:

```sh
make compose-images
```

The Dockerfiles reject an empty or malformed `VERSION` rather than publish a
mislabelled image. For a tagged direct Compose build, pass both facts explicitly:

```sh
test -z "$(git status --porcelain --untracked-files=normal)"
SOURCE_REVISION=$(git rev-parse HEAD) VERSION=$(git describe --tags --exact-match) docker compose -f docker-compose.yml.example build
```

## Container layout migration (OPS-04)

Since the `/opt/yago` layout landed, the container images use the same tree as
the deb/systemd deployments: binaries live in `/opt/yago/bin`, mutable state in
`/opt/yago/data` (`YAGO_DATA_DIR`), and node operator configuration in
`/opt/yago/etc`. The node declares data and configuration volumes. The crawler
declares its own writable data volume for persistent frontier checkpoints.

Migrating a deployment created before this layout (volume mounted at `/data`):

- **Recommended:** keep the same named volume and change only the mount target
  to `/opt/yago/data` (as `docker-compose.yml.example` now shows). A named
  volume's contents are independent of the container path, so the index and
  peer identity are preserved.
- **Alternative:** keep the old `/data` target and set `YAGO_DATA_DIR=/data` in
  the container environment.

Existing crawler containers had no durable volume. Before upgrading, let the
old crawler stop cleanly, then create or attach the separate
`yago-crawler-data:/opt/yago/data` volume shown in the Compose example. There is
no historical crawler checkpoint to migrate from a stateless container.

Custom entrypoint paths must switch from `/usr/local/bin/yago-node` and
custom legacy entrypoints to `/opt/yago/bin/{yago-node,yago-crawler}`.
