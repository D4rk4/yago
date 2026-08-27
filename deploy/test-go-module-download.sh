#!/bin/sh
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
temporary_directory=$(mktemp -d)
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM
fake_binary_directory="$temporary_directory/bin"
download_call_log="$temporary_directory/download-calls.log"
download_pause_log="$temporary_directory/download-pauses.log"
download_error_log="$temporary_directory/download-errors.log"
mkdir "$fake_binary_directory"

printf '%s\n' \
	'#!/bin/sh' \
	'set -eu' \
	'printf "%s\\n" "$*" >> "$MODULE_DOWNLOAD_TEST_CALL_LOG"' \
	'download_call_total=$(wc -l < "$MODULE_DOWNLOAD_TEST_CALL_LOG")' \
	'if [ "$download_call_total" -le "$MODULE_DOWNLOAD_TEST_FAILURES" ]; then' \
	'    exit 1' \
	'fi' \
	>"$fake_binary_directory/go"
printf '%s\n' \
	'#!/bin/sh' \
	'set -eu' \
	'printf "%s\\n" "$*" >> "$MODULE_DOWNLOAD_TEST_PAUSE_LOG"' \
	>"$fake_binary_directory/sleep"
chmod 755 "$fake_binary_directory/go" "$fake_binary_directory/sleep"

reset_download_observations() {
	: >"$download_call_log"
	: >"$download_pause_log"
	: >"$download_error_log"
}

run_module_download() {
	MODULE_DOWNLOAD_TEST_CALL_LOG="$download_call_log" \
		MODULE_DOWNLOAD_TEST_PAUSE_LOG="$download_pause_log" \
		MODULE_DOWNLOAD_TEST_FAILURES="$1" \
		PATH="$fake_binary_directory:$PATH" \
		sh "$here/download-go-modules.sh"
}

reset_download_observations
run_module_download 0
test "$(grep -cx 'mod download' "$download_call_log")" -eq 1
test ! -s "$download_pause_log"

reset_download_observations
run_module_download 1 2>"$download_error_log"
test "$(grep -cx 'mod download' "$download_call_log")" -eq 2
grep -qx '2' "$download_pause_log"
grep -Fqx \
	'Go module download failed; retrying (attempt=1 limit=3 delay_seconds=2)' \
	"$download_error_log"

reset_download_observations
if run_module_download 3 2>"$download_error_log"; then
	printf '%s\n' 'Persistent Go module download failure passed the retry boundary' >&2
	exit 1
fi
test "$(grep -cx 'mod download' "$download_call_log")" -eq 3
printf '%s\n' 2 4 >"$temporary_directory/expected-pauses.log"
cmp "$temporary_directory/expected-pauses.log" "$download_pause_log"
grep -Fqx \
	'Go module download failed (attempt=3 limit=3)' \
	"$download_error_log"
