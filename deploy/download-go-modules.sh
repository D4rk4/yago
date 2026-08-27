#!/bin/sh
set -eu

download_attempt=1
download_attempt_limit=3

while :; do
	if go mod download; then
		exit 0
	fi
	if [ "$download_attempt" -ge "$download_attempt_limit" ]; then
		printf '%s\n' \
			"Go module download failed (attempt=$download_attempt limit=$download_attempt_limit)" >&2
		exit 1
	fi
	download_pause_seconds=$((download_attempt * 2))
	printf '%s\n' \
		"Go module download failed; retrying (attempt=$download_attempt limit=$download_attempt_limit delay_seconds=$download_pause_seconds)" >&2
	sleep "$download_pause_seconds"
	download_attempt=$((download_attempt + 1))
done
