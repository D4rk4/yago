#!/bin/sh
set -eu

release_reference="${1:?release container reference}"
maximum_attempts=3
completed_attempts=0

while test "$completed_attempts" -lt "$maximum_attempts"; do
	completed_attempts=$((completed_attempts + 1))
	if push_output=$(docker push "$release_reference" 2>&1); then
		printf '%s\n' "$push_output" >&2
		exit 0
	else
		push_status=$?
	fi
	printf '%s\n' "$push_output" >&2
	if ! printf '%s\n' "$push_output" | grep -Eiq '(unknown blob|blob unknown to registry)'; then
		exit "$push_status"
	fi
	if test "$completed_attempts" -eq "$maximum_attempts"; then
		exit "$push_status"
	fi
	case "$completed_attempts" in
	1) retry_delay_seconds=2 ;;
	2) retry_delay_seconds=4 ;;
	esac
	printf 'release container child push retrying reference=%s completedAttempts=%s maximumAttempts=%s delaySeconds=%s reason=registry_blob_unavailable\n' \
		"$release_reference" \
		"$completed_attempts" \
		"$maximum_attempts" \
		"$retry_delay_seconds" >&2
	sleep "$retry_delay_seconds"
done
