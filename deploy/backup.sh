#!/bin/sh
set -eu

mode="${1:?usage: backup.sh docker|systemd ...}"
stamp=$(date -u +%Y%m%dT%H%M%SZ)
volume_archive_image="alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"
restart_mode=""
restart_compose_file=""
restart_node=""
restart_crawler=""
backup_complete=""

finish_backup() {
	status=$?
	trap - EXIT
	restart_failed=""
	node_restart_failed=""

	case "$restart_mode" in
	docker)
		if [ -n "$restart_node" ]; then
			if ! docker compose -f "$restart_compose_file" start "$restart_node"; then
				echo "failed to restart Docker service: $restart_node" >&2
				restart_failed=1
				node_restart_failed=1
			fi
		fi
		if [ -n "$restart_crawler" ] && [ -z "$node_restart_failed" ]; then
			if ! docker compose -f "$restart_compose_file" start "$restart_crawler"; then
				echo "failed to restart Docker service: $restart_crawler" >&2
				restart_failed=1
			fi
		fi
		;;
	systemd)
		if [ -n "$restart_node" ]; then
			if ! systemctl start "$restart_node"; then
				echo "failed to restart systemd unit: $restart_node" >&2
				restart_failed=1
				node_restart_failed=1
			fi
		fi
		if [ -n "$restart_crawler" ] && [ -z "$node_restart_failed" ]; then
			if ! systemctl start "$restart_crawler"; then
				echo "failed to restart systemd unit: $restart_crawler" >&2
				restart_failed=1
			fi
		fi
		;;
	esac

	if [ "$status" -eq 0 ] && [ -n "$restart_failed" ]; then
		status=1
	fi
	if [ "$status" -eq 0 ] && [ -n "$backup_complete" ]; then
		echo "backup written: yago-backup-$stamp.tar.gz"
	fi
	exit "$status"
}

trap finish_backup EXIT

case "$mode" in
docker)
	compose_file="${2:?compose file}"
	node_service="${3:?node service name}"
	node_volume="${4:?node data volume}"
	output_directory="${5:?output directory}"
	crawler_service="${6:-}"
	crawler_volume="${7:-}"
	if [ -n "$crawler_service" ] || [ -n "$crawler_volume" ]; then
		[ -n "$crawler_service" ] && [ -n "$crawler_volume" ] || {
			echo "crawler service and volume must be supplied together" >&2
			exit 64
		}
	fi
	mkdir -p "$output_directory"
	restart_mode=docker
	restart_compose_file="$compose_file"
	node_running=$(docker compose -f "$compose_file" ps --status running -q "$node_service")
	if [ -n "$crawler_service" ] || [ -n "$crawler_volume" ]; then
		crawler_running=$(docker compose -f "$compose_file" ps --status running -q "$crawler_service")
		if [ -n "$node_running" ]; then
			restart_node="$node_service"
		fi
		if [ -n "$crawler_running" ]; then
			restart_crawler="$crawler_service"
			docker compose -f "$compose_file" stop "$crawler_service"
		fi
		if [ -n "$restart_node" ]; then
			docker compose -f "$compose_file" stop "$node_service"
		fi
		docker run --rm -v "$node_volume":/node:ro \
			-v "$crawler_volume":/crawler:ro \
			-v "$output_directory":/backup "$volume_archive_image" \
			tar -czf "/backup/yago-backup-$stamp.tar.gz" -C / node crawler
	else
		if [ -n "$node_running" ]; then
			restart_node="$node_service"
		fi
		if [ -n "$restart_node" ]; then
			docker compose -f "$compose_file" stop "$node_service"
		fi
		docker run --rm -v "$node_volume":/data:ro \
			-v "$output_directory":/backup "$volume_archive_image" \
			tar -czf "/backup/yago-backup-$stamp.tar.gz" -C /data .
	fi
	backup_complete=1
	;;
systemd)
	node_unit="${2:?node systemd unit}"
	data_directory="${3:?data directory}"
	output_directory="${4:?output directory}"
	crawler_unit="${5:-}"
	mkdir -p "$output_directory"
	restart_mode=systemd
	if systemctl is-active --quiet "$node_unit"; then
		restart_node="$node_unit"
	fi
	if [ -n "$crawler_unit" ]; then
		if systemctl is-active --quiet "$crawler_unit"; then
			restart_crawler="$crawler_unit"
			systemctl stop "$crawler_unit"
		fi
	fi
	if [ -n "$restart_node" ]; then
		systemctl stop "$node_unit"
	fi
	tar -czf "$output_directory/yago-backup-$stamp.tar.gz" \
		-C "$data_directory" .
	backup_complete=1
	;;
*)
	echo "unknown mode: $mode (want docker or systemd)" >&2
	exit 64
	;;
esac
