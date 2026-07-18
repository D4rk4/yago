# Backup and restore (OPS-03)

Durable state includes the node's vault and search index, the dedicated
`${YAGO_DATA_DIR}/crawlbroker.db`, and the crawler's frontier checkpoint.
`crawlbroker.db` contains the node-side order queue, leases, settlements,
controls, and terminal-run delivery state. Bare-metal and package deployments
keep all of them under `/opt/yago/data`, owned by the `yago` user. Docker keeps
node and crawler state in separate named volumes because the images use
different unprivileged UIDs; both volumes are mounted at `/opt/yago/data` inside
their respective containers. Secrets are stored hashed, but an archive contains
the node's private identity and must be protected accordingly.

## Taking a backup

The dedicated broker database, retained main-vault metadata, and frontier
checkpoint describe one crawl state, so every service that is currently running
must be stopped for the complete copy. The script records the initial service
state, stops the active crawler before the active node, archives both durable
stores, then restarts only those services that were initially running, with the
node before the crawler. A failed copy still runs the restart path. A required
restart failure returns a nonzero status and suppresses the success message; a
crawler is not started after its node fails to restart.

```sh
# Docker Compose deployment
deploy/backup.sh docker docker-compose.yml \
  yago-node yago_yago-data /srv/backups \
  yago-crawler yago_yago-crawler-data

# systemd deployment
deploy/backup.sh systemd \
  yago-node.service /opt/yago/data /srv/backups yago-crawler.service
```

Compose prefixes named volumes with its project name by default. Confirm the
actual names with `docker volume ls` before running the command. The Docker
archive contains separate top-level `node/` and `crawler/` trees. The systemd
archive contains the shared data directory. Expect search and crawling to be
offline for the duration of the copy.

## Restoring

A restore validates the complete gzip/tar archive and its expected flat or
dual-volume layout before stopping a service. It extracts into staging, then
replaces each durable tree as one rollback-protected operation so an unreadable,
unsafe, mismatched, or failed restore leaves the previous data intact:

```sh
deploy/restore.sh docker docker-compose.yml \
  yago-node yago_yago-data /srv/backups/yago-backup-<stamp>.tar.gz \
  yago-crawler yago_yago-crawler-data
deploy/restore.sh systemd \
  yago-node.service /opt/yago/data /srv/backups/yago-backup-<stamp>.tar.gz \
  yago-crawler.service
```

The Docker restore resets node ownership to UID 65532 and crawler ownership to
UID 65534 before starting services. On bare metal it preserves the existing
data-directory owner and mode. The systemd restore requires an existing absolute
data path and rejects the filesystem root and top-level operating-system
directories. Both modes restore only services that were running when the
operation began, including after a failed replacement. A node restart failure
returns a nonzero status and prevents the crawler from starting against an
unavailable node; the final success message appears only after all required
restarts succeed.
Check the node log for shard integrity findings, confirm `/health` answers 200
and a local search returns documents, then confirm the crawler log reports
frontier recovery without a checkpoint error. When crawling is enabled, also
confirm `/ready` succeeds and the authenticated `/metrics` output contains
`crawl_broker_state_used_bytes` and `crawl_broker_state_file_bytes`.

Never restore only `crawlbroker.db`, only the main node vault, or only the
crawler checkpoint. That can combine orders, leases, settlements, and page
frontier state from different moments. The correct restart order after restore
is node first, readiness second, and crawler last; the crawler then adopts the
matching session-aware leases before receiving new work.

Keep the stopped backup taken immediately before a storage-layout upgrade for
the entire rollback window. The append-ordered document layout is readable by
newer binaries, but an older binary ignores its new partition and must never run
against the upgraded directory. To roll back, stop both services, restore that
pre-upgrade archive, install the older package, then start the node before the
crawler.

The first current node startup with crawling enabled copies one frozen
version-1 set of legacy broker buckets into `crawlbroker.db`, verifies it, and
leaves the source rows unchanged. After cutover those source rows are stale and
are not a live mirror. Deleting only the dedicated file makes a later current
startup import that stale cutover state again; an in-place downgrade makes the
older node use it directly. Either can resurrect work already settled after
cutover. The retained migration copies historical rows exactly and does not
promise to repair a legacy cross-bucket transition that was already
inconsistent. Restore the complete coordinated pre-upgrade archive for rollback
instead of deleting the file or downgrading against live upgraded data.

## Historical archives

The legacy node-only command remains accepted for restoring historical flat
archives. New backups must use the crawler arguments shown above so unfinished
crawl work is not omitted. A historical archive has no dedicated node broker
file; a current node creates it from the retained legacy state on first enabled
startup. It also has no crawler checkpoint, so it cannot recover work that the
old stateless crawler held only in memory.
