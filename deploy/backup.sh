#!/bin/sh
# Consistent offline backup of a yago node's data directory (OPS-03).
#
# The vault shards and the bleve index are only guaranteed consistent when
# the node is not writing, so the node is stopped for the copy (maintenance
# mode) and started again afterwards. Everything durable lives in the two
# trees this archives: the sharded vault (identity, settings, API-key hashes,
# ranking profiles, event log, documents, postings) and the sharded search
# index. Volatile state — crawl frontiers, in-flight queues, session caches —
# is in memory by design and is deliberately not part of a backup. Secrets
# are stored hashed; the archive never contains a plaintext key, but it does
# contain the node's private identity — treat it like one.
#
# Usage:
#   backup.sh docker  <compose-file> <service> <volume> <output-dir>
#   backup.sh systemd <unit> <data-dir> <output-dir>
set -eu

mode="${1:?usage: backup.sh docker|systemd ...}"
stamp=$(date -u +%Y%m%dT%H%M%SZ)

case "$mode" in
docker)
	compose="${2:?compose file}"
	service="${3:?service name}"
	volume="${4:?data volume}"
	outdir="${5:?output dir}"
	mkdir -p "$outdir"
	docker compose -f "$compose" stop "$service"
	trap 'docker compose -f "$compose" start "$service"' EXIT
	docker run --rm -v "$volume":/data:ro -v "$outdir":/backup alpine \
		tar -czf "/backup/yago-backup-$stamp.tar.gz" -C /data .
	;;
systemd)
	unit="${2:?systemd unit}"
	datadir="${3:?data dir}"
	outdir="${4:?output dir}"
	mkdir -p "$outdir"
	systemctl stop "$unit"
	trap 'systemctl start "$unit"' EXIT
	tar -czf "$outdir/yago-backup-$stamp.tar.gz" -C "$datadir" .
	;;
*)
	echo "unknown mode: $mode (want docker or systemd)" >&2
	exit 64
	;;
esac

echo "backup written: yago-backup-$stamp.tar.gz"
