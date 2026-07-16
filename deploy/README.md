# Bare-metal deployment (systemd + Debian package)

**Quick paths:** `deploy/install.sh` installs from a release tarball;
`deploy/debian/build-deb.sh` builds the `.deb` (the release workflow publishes
both per tag, amd64 + arm64); `deploy/backup.sh` / `deploy/restore.sh` cover
disaster recovery (doc/backup-restore.md).

The same `yago-node` and `yagocrawler` binaries that run under Docker also run
directly on a Linux host, managed by systemd and installed from a Debian
package. This directory holds the reference systemd units and environment files;
`docker-compose.yml.example` is the equivalent reference for the container
deployment, and the two are kept in step.

## Filesystem layout

The Debian package and the systemd units share one layout:

| Path | Contents |
| --- | --- |
| `/opt/yago/bin` | the `yago-node` and `yagocrawler` binaries |
| `/opt/yago/etc` | the environment files below |
| `/opt/yago/data` | durable node state (indexes and vaults) |

The package creates a system `yago` user that owns `/opt/yago/data`. Removing the
package leaves that directory intact.

The crawler creates throwaway Firefox profiles in its private process temporary
directory. They are not durable state. `YAGOCRAWLER_WORKER_ID` is likewise a display-name prefix;
each crawler process appends a random suffix so simultaneous workers never share
a lease identity. `YAGOCRAWLER_MAX_PAGES_PER_RUN` bounds lossless pending work per
run at 50,000 pages by default; `0` removes that safety bound.

## Package dependencies

The binaries rely on facilities the operating system provides, installed as
package dependencies rather than bundled:

- `ca-certificates` — trust roots for outbound TLS. The binaries read the system
  trust store; there is no baked-in certificate bundle on bare metal.
- `firefox-esr` on Debian (or `firefox` on Ubuntu/Fedora) — the crawler's
  slow-path browser, driven headless over Marionette. The container image
  bundles `firefox-esr`; a host install discovers the OS browser on `PATH`, while
  `YAGOCRAWLER_BROWSER_PATH` can pin a non-standard location. The crawler still runs without it, serving
  only the HTTP fast path, so it is a recommended, not a required, dependency. A
  browser path left pointing at Chromium/Chrome — a leftover from before the
  crawler moved to Firefox — is ignored with a startup warning, and the crawler
  discovers Firefox on `PATH` instead, since the slow path is driven over
  Firefox-only Marionette.

## Install and run

`deploy/install.sh` performs the whole layout: it creates the `yago` system
user, the `/opt/yago/{bin,etc,data}` tree with correct ownership, copies the
binaries and env examples (never overwriting an edited env), installs the
units, and enables them when systemd is running. Idempotent — re-run it for
upgrades:

```sh
sudo deploy/install.sh <dir-with-binaries>
```

Or by hand:

```sh
sudo cp yago-node yagocrawler /opt/yago/bin/
sudo cp deploy/systemd/yago-node.service deploy/systemd/yagocrawler.service /etc/systemd/system/
sudo cp deploy/systemd/yago-node.env.example /opt/yago/etc/yago-node.env
sudo cp deploy/systemd/yagocrawler.env.example /opt/yago/etc/yagocrawler.env
# edit the two env files, then:
sudo systemctl daemon-reload
sudo systemctl enable --now yago-node
sudo systemctl enable --now yagocrawler   # optional crawler worker
```

## The browser sandbox on bare metal

Headless Firefox has its own content-process sandbox. It needs unprivileged user
namespaces, which the container image and most current Linux hosts (Ubuntu
23.10+, AppArmor userns restrictions) do not grant, so the crawler defaults to
`YAGOCRAWLER_BROWSER_SANDBOX=false` and launches Firefox with the content sandbox
disabled (`MOZ_DISABLE_CONTENT_SANDBOX=1`). On bare metal the systemd unit is the
isolation boundary — it runs the crawler as an unprivileged user with
`NoNewPrivileges`, a private `/tmp`, and a read-only system — and the crawler is
already egress-guarded against private networks.

An operator on a host that supports the browser sandbox can opt back in by
setting `YAGOCRAWLER_BROWSER_SANDBOX=true` **and** relaxing the unit
(`NoNewPrivileges=no`, and allow user namespaces); Firefox cannot start its
content sandbox under `NoNewPrivileges`.

## Crawler resource limits

`YAGOCRAWLER_WORKERS` is a bootstrap value in both service environment files
and must be kept equal. It is the exact number of page-fetch workers in each
connected `yagocrawler` process, not a crawl-run or task limit, and accepts
values from 1 through 256. After a crawler's first heartbeat, the persisted
Configuration → Crawler value on `yago-node` becomes authoritative and is sent
to every connected crawler. A live resize stops new page intake, lets the
current in-flight fetches finish, and then starts the requested worker group;
the crawler process does not restart. With several crawler processes the value
applies independently to each process, so aggregate fetch concurrency is the
per-process setting multiplied by the number of connected processes.

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
order payload.

`yagocrawler.service` applies cgroup controls to bound headless-Firefox memory
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
systemctl edit yagocrawler
# [Service]
# MemoryHigh=2G
# MemoryMax=3G
```

A small single-purpose box can raise the shares; a box that co-hosts other
services can lower them or switch to absolute sizes as above.

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
an interrupted attempt, so a partial index is never served. Rebuild writes use
16-document batches to limit transient segment memory, but startup downtime,
merge I/O, and temporary disk use still scale with the stored corpus.

For a bare-metal package upgrade, back up the data directory, stop both services,
install the published package, and start `yago-node` first while watching its
journal, RSS, and free disk space. Start `yagocrawler` only after the node is
ready. Do not use the normal restart window for a mapping-changing release until
the same corpus size has been timed on representative storage and the maintenance
window covers the measured rebuild.

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

### `yagoseek.dev` production exception

The operator policy for deployments performed as `root@yagoseek.dev` skips a
new pre-upgrade backup to keep the outage bounded. Retain every existing archive
under `/opt/yago/backups` unchanged; do not delete, replace, prune, or reuse one
as staging space. Download the release package under
`/opt/yago/releases/vX.Y.Z`, verify its attestation and package identity there,
stop `yagocrawler` and `yago-node`, install the package, then start and verify the
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
`yagocrawler --version` report it), each arch ships as a tarball (binaries + install.sh +
units + backup doc), a `.deb`, and an `.rpm`. The amd64 `.deb` is smoke-installed
across Debian 12/13 and Ubuntu 24.04 + `ubuntu:latest`, and the amd64 `.rpm`
across Fedora and Rocky 9 — each run checks the declared dependencies resolve,
both binaries report the stamped version, and package removal keeps
`/opt/yago/data`. Every tarball, Debian package, and RPM package receives
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
or date aliases. The one-time v0.0.10 backfill uses a `workflow_dispatch` run
from `main` with its release ID, tag, tag ref, annotated tag object, and source
commit fixed in the workflow. It verifies that identity and main ancestry,
then checks out the historical source while package construction and GitHub
Release creation remain disabled. It cannot move the tag, replace an existing
release manifest, rebuild package assets, or recreate the GitHub Release. Its
repair path consumes only the pinned successful native-validation artifacts
from its first run. Standard GitHub Actions provenance records the current
workflow invocation, while a separate signed identity predicate records the
historical release, tag object, source, and validation run. Build development images locally with
`make compose-images`.

## Published container images

Every container release publishes these public manifest lists for Linux amd64
and arm64:

- `ghcr.io/d4rk4/yago-node:vX.Y.Z`;
- `ghcr.io/d4rk4/yagocrawler:vX.Y.Z`.

Images published with the repository `GITHUB_TOKEN` and OCI source label are
expected to inherit the public repository's visibility. The publication gate
uses an empty Docker credential directory to pull both the exact tag and its
digest before it can succeed. If inheritance does not make a package public,
an owner must change visibility through the package settings UI and retry; the
package REST and GraphQL APIs do not expose a supported visibility mutation.
A retry accepts an existing architecture tag or manifest list only when its
image identity, labels, platforms, and child digests match the validated
archives; registry authorization, network, and server failures stop publication
rather than being interpreted as a missing tag.

After that gate passes, select the exact version and verify the embedded product
identity before use:

```sh
version=vX.Y.Z
node_image=ghcr.io/d4rk4/yago-node
crawler_image=ghcr.io/d4rk4/yagocrawler
docker pull "$node_image:$version"
docker pull "$crawler_image:$version"
test "$(docker run --rm "$node_image:$version" --version)" = "yago-node $version"
test "$(docker run --rm "$crawler_image:$version" --version)" = "yago-crawler $version"
```

The release memo and GHCR package page record each multi-architecture
manifest-list digest. Pin that digest for a deployment so a tag lookup is not
part of future selection, then verify its GitHub-hosted provenance:

```sh
version=vX.Y.Z
node_image=ghcr.io/d4rk4/yago-node
crawler_image=ghcr.io/d4rk4/yagocrawler
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
docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }} {{ index .Config.Labels "org.opencontainers.image.source" }}' yagocrawler:latest
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
the deb/systemd deployments: the binaries live in `/opt/yago/bin`, the node's
mutable state in `/opt/yago/data` (`YAGO_DATA_DIR` default), and
operator-managed config files in `/opt/yago/etc`; both data and etc are
declared volumes. The crawler container is stateless and only moves its binary.

Migrating a deployment created before this layout (volume mounted at `/data`):

- **Recommended:** keep the same named volume and change only the mount target
  to `/opt/yago/data` (as `docker-compose.yml.example` now shows). A named
  volume's contents are independent of the container path, so the index and
  peer identity are preserved.
- **Alternative:** keep the old `/data` target and set `YAGO_DATA_DIR=/data` in
  the container environment.

Custom entrypoint paths must switch from `/usr/local/bin/yago-node` and
`/usr/local/bin/yagocrawler` to `/opt/yago/bin/{yago-node,yagocrawler}`.
