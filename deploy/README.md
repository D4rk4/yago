# Bare-metal deployment (systemd + Debian package)

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
- `chromium` (or `google-chrome-stable`) — the crawler's slow-path browser. The
  container image bundles `headless-shell`; a host install points the crawler at
  the OS browser through `YAGOCRAWLER_BROWSER_PATH`.

## Install and run

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

Headless Chrome has its own renderer sandbox. It needs unprivileged user
namespaces, which the container image and most current Linux hosts (Ubuntu
23.10+, AppArmor userns restrictions) do not grant, so the crawler defaults to
`YAGOCRAWLER_BROWSER_SANDBOX=false` and launches Chrome with `--no-sandbox`. On
bare metal the systemd unit is the isolation boundary — it runs the crawler as an
unprivileged user with `NoNewPrivileges`, a private `/tmp`, and a read-only
system — and the crawler is already egress-guarded against private networks.

An operator on a host that supports the browser sandbox can opt back in by
setting `YAGOCRAWLER_BROWSER_SANDBOX=true` **and** relaxing the unit
(`NoNewPrivileges=no`, and allow user namespaces); Chrome cannot start its
sandbox under `NoNewPrivileges`.

## Debian package

The `.deb` build automation (which installs this layout, ships these units,
seeds the env files, and creates the `yago` user) is tracked as OPS-05 in
`PLAN.md`. The runtime is already deployment-agnostic, so the package only has to
place files and register the services.
