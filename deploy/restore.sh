#!/bin/sh
set -eu

mode="${1:?usage: restore.sh docker|systemd ...}"
volume_archive_image="alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"
temporary_directory=""
restart_mode=""
restart_compose_file=""
restart_node=""
restart_crawler=""
systemd_data_directory=""
systemd_old_directory=""
systemd_swap_started=""
systemd_swap_committed=""

cleanup_restore() {
	status=$?
	restart_failed=""
	node_restart_failed=""
	set +e
	trap - 0 HUP INT TERM
	if [ -n "$systemd_swap_started" ] && [ -z "$systemd_swap_committed" ] && \
		[ -d "$systemd_old_directory" ]; then
		rm -rf "$systemd_data_directory"
		mv "$systemd_old_directory" "$systemd_data_directory"
	fi
	if [ -n "$systemd_swap_committed" ] && [ -d "$systemd_old_directory" ]; then
		rm -rf "$systemd_old_directory"
	fi
	if [ -n "$temporary_directory" ] && [ -d "$temporary_directory" ]; then
		rm -rf "$temporary_directory"
	fi
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
	if [ "$status" -eq 0 ]; then
		echo "restore complete; check node integrity and crawler frontier recovery logs"
	fi
	exit "$status"
}

trap cleanup_restore 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

safe_data_directory() {
	candidate="$1"
	case "$candidate" in
	/*) ;;
	*)
		echo "systemd data directory must be an absolute path: $candidate" >&2
		return 64
		;;
	esac
	[ -d "$candidate" ] || {
		echo "systemd data directory does not exist: $candidate" >&2
		return 66
	}
	resolved=$(CDPATH= cd "$candidate" && pwd -P) || return 66
	case "$resolved" in
	/ | /bin | /boot | /dev | /etc | /home | /lib | /lib64 | /media | /mnt | /opt | /proc | /root | /run | /sbin | /srv | /sys | /tmp | /usr | /var)
		echo "refusing to restore over unsafe systemd data directory: $resolved" >&2
		return 64
		;;
	esac
	printf '%s\n' "$resolved"
}

archive_is_safe() {
	archive_path="$1"
	manifest_path="$2"
	if ! tar -tzf "$archive_path" >"$manifest_path"; then
		echo "backup archive is unreadable or truncated: $archive_path" >&2
		return 65
	fi
	if ! awk '
		BEGIN { found = 0; invalid = 0 }
		{
			path = $0
			if (substr(path, 1, 1) == "/") invalid = 1
			parts = split(path, component, "/")
			for (part_index = 1; part_index <= parts; part_index++) {
				if (component[part_index] == "..") invalid = 1
			}
			while (substr(path, 1, 2) == "./") path = substr(path, 3)
			if (path != "" && path != ".") {
				found = 1
				split(path, top, "/")
				if (top[1] == ".yago-restore-new" || top[1] == ".yago-restore-old") invalid = 1
			}
		}
		END { if (!found || invalid) exit 1 }
	' "$manifest_path"; then
		echo "backup archive contains an unsafe or empty path set: $archive_path" >&2
		return 65
	fi
	if ! tar -tvzf "$archive_path" | awk '
		{
			kind = substr($0, 1, 1)
			if (kind != "-" && kind != "d") exit 1
		}
	'; then
		echo "backup archive contains unsupported links or special files: $archive_path" >&2
		return 65
	fi
}

validate_archive_layout() {
	content_directory="$1"
	layout="$2"
	found_node=""
	found_crawler=""
	entries=0
	for entry in "$content_directory"/* "$content_directory"/.[!.]* "$content_directory"/..?*; do
		[ -e "$entry" ] || [ -L "$entry" ] || continue
		entries=$((entries + 1))
		case "$(basename "$entry")" in
		node) found_node=1 ;;
		crawler) found_crawler=1 ;;
		esac
		if [ "$layout" = dual ]; then
			case "$(basename "$entry")" in
			node | crawler) ;;
			*)
				echo "dual-volume backup has an unexpected top-level entry" >&2
				return 65
				;;
			esac
		fi
	done
	[ "$entries" -gt 0 ] || {
		echo "backup archive is empty" >&2
		return 65
	}
	if [ "$layout" = dual ]; then
		[ "$entries" -eq 2 ] && [ -n "$found_node" ] && [ -n "$found_crawler" ] && \
			[ -d "$content_directory/node" ] && [ -d "$content_directory/crawler" ] || {
			echo "dual-volume backup must contain only node/ and crawler/" >&2
			return 65
		}
	elif [ "$entries" -eq 2 ] && [ -n "$found_node" ] && [ -n "$found_crawler" ] && \
		[ -d "$content_directory/node" ] && [ -d "$content_directory/crawler" ]; then
		echo "dual-volume backup cannot be restored as a flat data directory" >&2
		return 65
	fi
}

stage_archive() {
	archive_path="$1"
	layout="$2"
	stage_parent="$3"
	temporary_directory=$(mktemp -d "$stage_parent/.yago-restore.XXXXXX")
	manifest_path="$temporary_directory/manifest"
	content_directory="$temporary_directory/content"
	mkdir "$content_directory"
	archive_is_safe "$archive_path" "$manifest_path"
	tar --no-same-owner -xzf "$archive_path" -C "$content_directory"
	rm -f "$manifest_path"
	validate_archive_layout "$content_directory" "$layout"
}

restore_single_volume() {
	volume="$1"
	stage="$2"
	owner="$3"
	docker run --rm -v "$volume":/target -v "$stage":/stage:ro \
		"$volume_archive_image" sh -c '
set -eu
target=/target
new=/target/.yago-restore-new
old=/target/.yago-restore-old
old_complete=0
committed=0
rollback() {
	status=$?
	trap - 0 HUP INT TERM
	if [ -d "$old" ]; then
		if [ "$old_complete" -eq 1 ]; then
			for entry in "$target"/* "$target"/.[!.]* "$target"/..?*; do
				[ -e "$entry" ] || [ -L "$entry" ] || continue
				[ "$entry" = "$old" ] || [ "$entry" = "$new" ] || rm -rf "$entry"
			done
		fi
		for entry in "$old"/* "$old"/.[!.]* "$old"/..?*; do
			[ -e "$entry" ] || [ -L "$entry" ] || continue
			mv "$entry" "$target"/
		done
		rmdir "$old" || true
	fi
	rm -rf "$new"
	[ "$committed" -eq 0 ] || rm -rf "$old"
	exit "$status"
}
trap rollback 0
trap "exit 129" HUP
trap "exit 130" INT
trap "exit 143" TERM
[ ! -e "$new" ] && [ ! -e "$old" ]
mkdir "$new"
cp -a /stage/. "$new"/
chown -R "$1" "$new"
mkdir "$old"
for entry in "$target"/* "$target"/.[!.]* "$target"/..?*; do
	[ -e "$entry" ] || [ -L "$entry" ] || continue
	[ "$entry" = "$new" ] || [ "$entry" = "$old" ] || mv "$entry" "$old"/
done
old_complete=1
for entry in "$new"/* "$new"/.[!.]* "$new"/..?*; do
	[ -e "$entry" ] || [ -L "$entry" ] || continue
	mv "$entry" "$target"/
done
rmdir "$new"
committed=1
rm -rf "$old"
trap - 0 HUP INT TERM
' restore "$owner"
}

restore_dual_volumes() {
	node_volume="$1"
	crawler_volume="$2"
	node_stage="$3"
	crawler_stage="$4"
	docker run --rm -v "$node_volume":/node -v "$crawler_volume":/crawler \
		-v "$node_stage":/node-stage:ro -v "$crawler_stage":/crawler-stage:ro \
		"$volume_archive_image" sh -c '
set -eu
node_old_complete=0
crawler_old_complete=0
committed=0
move_entries() {
	from="$1"
	to="$2"
	for entry in "$from"/* "$from"/.[!.]* "$from"/..?*; do
		[ -e "$entry" ] || [ -L "$entry" ] || continue
		mv "$entry" "$to"/
	done
}
clear_except_restore() {
	root="$1"
	for entry in "$root"/* "$root"/.[!.]* "$root"/..?*; do
		[ -e "$entry" ] || [ -L "$entry" ] || continue
		case "$entry" in
		"$root/.yago-restore-new" | "$root/.yago-restore-old") ;;
		*) rm -rf "$entry" ;;
		esac
	done
}
rollback() {
	status=$?
	trap - 0 HUP INT TERM
	if [ -d /crawler/.yago-restore-old ]; then
		[ "$crawler_old_complete" -eq 0 ] || clear_except_restore /crawler
		move_entries /crawler/.yago-restore-old /crawler
		rmdir /crawler/.yago-restore-old || true
	fi
	if [ -d /node/.yago-restore-old ]; then
		[ "$node_old_complete" -eq 0 ] || clear_except_restore /node
		move_entries /node/.yago-restore-old /node
		rmdir /node/.yago-restore-old || true
	fi
	rm -rf /node/.yago-restore-new /crawler/.yago-restore-new
	if [ "$committed" -eq 1 ]; then
		rm -rf /node/.yago-restore-old /crawler/.yago-restore-old
	fi
	exit "$status"
}
trap rollback 0
trap "exit 129" HUP
trap "exit 130" INT
trap "exit 143" TERM
[ ! -e /node/.yago-restore-new ] && [ ! -e /node/.yago-restore-old ]
[ ! -e /crawler/.yago-restore-new ] && [ ! -e /crawler/.yago-restore-old ]
mkdir /node/.yago-restore-new /crawler/.yago-restore-new
cp -a /node-stage/. /node/.yago-restore-new/
cp -a /crawler-stage/. /crawler/.yago-restore-new/
chown -R 65532:65532 /node/.yago-restore-new
chown -R 65534:65534 /crawler/.yago-restore-new
mkdir /node/.yago-restore-old /crawler/.yago-restore-old
for entry in /node/* /node/.[!.]* /node/..?*; do
	[ -e "$entry" ] || [ -L "$entry" ] || continue
	case "$entry" in
	/node/.yago-restore-new | /node/.yago-restore-old) ;;
	*) mv "$entry" /node/.yago-restore-old/ ;;
	esac
done
node_old_complete=1
for entry in /crawler/* /crawler/.[!.]* /crawler/..?*; do
	[ -e "$entry" ] || [ -L "$entry" ] || continue
	case "$entry" in
	/crawler/.yago-restore-new | /crawler/.yago-restore-old) ;;
	*) mv "$entry" /crawler/.yago-restore-old/ ;;
	esac
done
crawler_old_complete=1
move_entries /node/.yago-restore-new /node
move_entries /crawler/.yago-restore-new /crawler
rmdir /node/.yago-restore-new /crawler/.yago-restore-new
committed=1
rm -rf /node/.yago-restore-old /crawler/.yago-restore-old
trap - 0 HUP INT TERM
'
}

case "$mode" in
docker)
	compose_file="${2:?compose file}"
	node_service="${3:?node service name}"
	node_volume="${4:?node data volume}"
	archive_path="${5:?backup archive}"
	crawler_service="${6:-}"
	crawler_volume="${7:-}"
	[ -f "$archive_path" ] || {
		echo "no such archive: $archive_path" >&2
		exit 66
	}
	if [ -n "$crawler_service" ] || [ -n "$crawler_volume" ]; then
		[ -n "$crawler_service" ] && [ -n "$crawler_volume" ] || {
			echo "crawler service and volume must be supplied together" >&2
			exit 64
		}
		stage_archive "$archive_path" dual "${TMPDIR:-/tmp}"
	else
		stage_archive "$archive_path" flat "${TMPDIR:-/tmp}"
	fi
	restart_mode=docker
	restart_compose_file="$compose_file"
	if [ -n "$(docker compose -f "$compose_file" ps --status running -q "$node_service")" ]; then
		restart_node="$node_service"
	fi
	if [ -n "$crawler_service" ] && \
		[ -n "$(docker compose -f "$compose_file" ps --status running -q "$crawler_service")" ]; then
		restart_crawler="$crawler_service"
	fi
	if [ -n "$crawler_service" ]; then
		docker compose -f "$compose_file" stop "$crawler_service"
	fi
	docker compose -f "$compose_file" stop "$node_service"
	if [ -n "$crawler_service" ]; then
		restore_dual_volumes \
			"$node_volume" "$crawler_volume" \
			"$temporary_directory/content/node" \
			"$temporary_directory/content/crawler"
	else
		restore_single_volume "$node_volume" "$temporary_directory/content" 65532:65532
	fi
	;;
systemd)
	node_unit="${2:?node systemd unit}"
	data_directory=$(safe_data_directory "${3:?data directory}")
	archive_path="${4:?backup archive}"
	crawler_unit="${5:-}"
	[ -f "$archive_path" ] || {
		echo "no such archive: $archive_path" >&2
		exit 66
	}
	stage_archive "$archive_path" flat "$(dirname "$data_directory")"
	owner=$(stat -c '%u:%g' "$data_directory")
	mode_bits=$(stat -c '%a' "$data_directory")
	chown -R "$owner" "$temporary_directory/content"
	chmod "$mode_bits" "$temporary_directory/content"
	restart_mode=systemd
	if systemctl is-active --quiet "$node_unit"; then
		restart_node="$node_unit"
	fi
	if [ -n "$crawler_unit" ] && systemctl is-active --quiet "$crawler_unit"; then
		restart_crawler="$crawler_unit"
	fi
	if [ -n "$crawler_unit" ]; then
		systemctl stop "$crawler_unit"
	fi
	systemctl stop "$node_unit"
	systemd_data_directory="$data_directory"
	systemd_old_directory="$data_directory.yago-restore-old.$$"
	[ ! -e "$systemd_old_directory" ] || {
		echo "restore rollback path already exists: $systemd_old_directory" >&2
		exit 73
	}
	systemd_swap_started=1
	mv "$data_directory" "$systemd_old_directory"
	mv "$temporary_directory/content" "$data_directory"
	systemd_swap_committed=1
	;;
*)
	echo "unknown mode: $mode (want docker or systemd)" >&2
	exit 64
	;;
esac
