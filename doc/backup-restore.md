# Backup and restore (OPS-03)

Everything durable lives under the node's data directory in two shard trees:
`yago-node.db.vault` (identity, settings, API-key hashes, ranking profiles,
the event log, documents, postings) and `search.bleve` (the search index).
Volatile state — crawl frontiers, in-flight queues, session caches — is in
memory by design and is not part of a backup. Secrets are stored hashed;
an archive never contains a plaintext API key, but it does contain the
node's **private identity** — anyone holding it can impersonate the peer,
so store archives accordingly.

## Taking a backup

The shard trees are only consistent when the node is not writing, so the
scripts stop the node for the copy (maintenance mode) and always start it
again, even on failure:

```sh
# Docker Compose deployment
deploy/backup.sh docker docker-compose.yml yago-node yago_yago-data /srv/backups

# systemd deployment
deploy/backup.sh systemd yago-node.service /var/lib/yago /srv/backups
```

Expect the node to be offline for the duration of the tar (seconds to
minutes, proportional to index size).

## Restoring

A restore wipes the target first — mixing shard trees from two moments
corrupts both — then unpacks and restarts:

```sh
deploy/restore.sh docker docker-compose.yml yago-node yago_yago-data /srv/backups/yago-backup-<stamp>.tar.gz
deploy/restore.sh systemd yago-node.service /var/lib/yago /srv/backups/yago-backup-<stamp>.tar.gz
```

On start the node runs its shard integrity checks (STOR-04) and heals index
orphans (SEARCH-30) — that pass is the restore verification. Check the log
for integrity findings, then confirm `/health` on the ops listener answers
200 and a `resource=local` search returns documents.

Keep the stopped backup taken immediately before a storage-layout upgrade for
the entire rollback window. The append-ordered document layout is readable by
newer binaries, but an older binary ignores its new partition and must never run
against the upgraded directory. To roll back, stop both services, restore that
pre-upgrade archive, install the older package, then start the node before the
crawler.

## Validated end to end

The flow above was exercised against a live node: backup taken (≈100 MB
archive of a ~225 MB data dir), volume wiped, archive restored, node
restarted — health 200, local search served restored documents, no
integrity findings.
