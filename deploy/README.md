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
| `/opt/yago/data` | all mutable state (indexes, vaults, the crawler's browser profile) |

The package creates a system `yago` user that owns `/opt/yago/data`. Removing the
package leaves that directory intact.

## Package dependencies

The binaries rely on facilities the operating system provides, installed as
package dependencies rather than bundled:

- `ca-certificates` — trust roots for outbound TLS. The binaries read the system
  trust store; there is no baked-in certificate bundle on bare metal.
- `firefox-esr` on Debian (or `firefox` on Ubuntu/Fedora) — the crawler's
  slow-path browser, driven headless over Marionette. The container image
  bundles `firefox-esr`; a host install points the crawler at the OS browser
  through `YAGOCRAWLER_BROWSER_PATH`. The crawler still runs without it, serving
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

`yagocrawler.service` caps the headless-Firefox fetcher through cgroup controls
so a runaway render cannot starve the co-located node: `MemoryHigh=60%` applies
reclaim pressure (it throttles, never kills), `MemoryMax=85%` is a hard ceiling
whose out-of-memory kill stays confined to the crawler cgroup (the node
survives, systemd restarts the crawler, and the browser circuit-breaker degrades
gracefully in the meantime), `TasksMax=4096` bounds thread/process explosion,
and `CPUWeight=50` lets the node win the CPU under contention so search and admin
stay responsive during a crawl. On top of the cgroup weights the unit runs the
crawler *process* at the lowest scheduling priority — `Nice=19` (lowest CPU nice)
and `IOSchedulingClass=idle` (lowest disk-I/O class, honored by the BFQ I/O
scheduler), with `IOWeight` capping its cgroup I/O share — so a crawl yields CPU
and disk to a search the node is answering; `CPUWeight` stays the primary
cross-cgroup CPU lever and can be lowered further to give the node an even larger
share. The percentages are relative to physical RAM, so they scale to any box. Tune them per host with a drop-in rather than editing the
shipped unit:

```
systemctl edit yagocrawler
# [Service]
# MemoryHigh=2G
# MemoryMax=3G
```

A small single-purpose box can raise the shares; a box that co-hosts other
services can lower them or switch to absolute sizes as above.

## Debian package

The `.deb` build automation (which installs this layout, ships these units,
seeds the env files, and creates the `yago` user) lives in
[debian/](debian/) and runs in the release pipeline. The runtime is
deployment-agnostic, so the package only has to place files and register the
services.

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

Pushing a `v*` tag runs `.github/workflows/release.yml`: `make verify` gates
the release, binaries build for amd64 and arm64 (CGO off, trimmed) with the tag
stamped in as the build version (`yago-node --version` / `yagocrawler
--version` report it), each arch ships as a tarball (binaries + install.sh +
units + backup doc), a `.deb`, and an `.rpm`. The amd64 `.deb` is smoke-installed
across Debian 12/13 and Ubuntu 24.04 + `ubuntu:latest`, and the amd64 `.rpm`
across Fedora and Rocky 9 — each run checks the declared dependencies resolve,
both binaries report the stamped version, and package removal keeps
`/opt/yago/data`. Release notes are generated from the commit titles since the
previous tag, and a GitHub Release carries the assets. Container images are not
published by CI; build them locally from the Dockerfiles (`docker compose
build`) when a container deployment is wanted.

## Container build provenance

Both product Dockerfiles pin the Go builder and final runtime base images by
SHA-256 digest. Base-image changes are therefore explicit source changes rather
than mutable-tag resolution at build time. The readable tags remain beside the
digests so operators can see the selected release.

Set `SOURCE_REVISION` explicitly when building a source checkout. Compose passes
it to both product images and each final image records it in the
`org.opencontainers.image.revision` label; the source repository is recorded in
`org.opencontainers.image.source`.

```sh
SOURCE_REVISION=$(git rev-parse HEAD) docker compose build
docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }} {{ index .Config.Labels "org.opencontainers.image.source" }}' yago-node:latest
docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }} {{ index .Config.Labels "org.opencontainers.image.source" }}' yagocrawler:latest
```

The default revision is `unknown`, which makes an omitted caller stamp visible
instead of guessing from a possibly dirty worktree. `SOURCE_REVISION` identifies
source provenance; the separate release `VERSION` build argument controls the
binary version reported by `--version`.

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
