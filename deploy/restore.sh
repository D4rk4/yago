#!/bin/sh
# Restore a yago node's data directory from a backup.sh archive (OPS-03).
#
# The target is wiped first: mixing shard trees from two moments corrupts
# both, so a restore is all-or-nothing. On start the node runs its shard
# integrity checks (STOR-04) and heals index orphans (SEARCH-30), which
# doubles as the restore verification — watch the logs after starting.
#
# Usage:
#   restore.sh docker  <compose-file> <service> <volume> <archive>
#   restore.sh systemd <unit> <data-dir> <archive>
set -eu

mode="${1:?usage: restore.sh docker|systemd ...}"

case "$mode" in
docker)
	compose="${2:?compose file}"
	service="${3:?service name}"
	volume="${4:?data volume}"
	archive="${5:?backup archive}"
	[ -f "$archive" ] || { echo "no such archive: $archive" >&2; exit 66; }
	docker compose -f "$compose" stop "$service"
	docker run --rm -v "$volume":/data -v "$(dirname "$archive")":/backup:ro alpine \
		sh -c "rm -rf /data/* && tar -xzf '/backup/$(basename "$archive")' -C /data && chown -R 65532:65532 /data"
	docker compose -f "$compose" start "$service"
	;;
systemd)
	unit="${2:?systemd unit}"
	datadir="${3:?data dir}"
	archive="${4:?backup archive}"
	[ -f "$archive" ] || { echo "no such archive: $archive" >&2; exit 66; }
	systemctl stop "$unit"
	rm -rf "${datadir:?}"/*
	tar -xzf "$archive" -C "$datadir"
	systemctl start "$unit"
	;;
*)
	echo "unknown mode: $mode (want docker or systemd)" >&2
	exit 64
	;;
esac

echo "restore complete; check the node log for shard integrity results"
