#!/bin/sh
set -eu

installation_root=""
systemctl_command=systemctl
if [ "${1:-}" = "--root" ]; then
	installation_root="${2:?installation root}"
	systemctl_command="${3:?systemctl command}"
fi

legacy_environment="$installation_root/opt/yago/etc/yagocrawler.env"
canonical_environment="$installation_root/opt/yago/etc/yago-crawler.env"

if [ -d "$installation_root/run/systemd/system" ]; then
	"$systemctl_command" stop yago-crawler.service yagocrawler.service 2>/dev/null || true
	"$systemctl_command" disable yagocrawler.service >/dev/null 2>&1 || true
fi

rm -f \
	"$installation_root/etc/systemd/system/yagocrawler.service" \
	"$installation_root/etc/systemd/system/multi-user.target.wants/yagocrawler.service" \
	"$installation_root/lib/systemd/system/yagocrawler.service" \
	"$installation_root/usr/lib/systemd/system/yagocrawler.service"

if [ -f "$legacy_environment" ]; then
	if [ ! -e "$canonical_environment" ]; then
		temporary_environment="$canonical_environment.migrate.$$"
		temporary_rewrite="$temporary_environment.rewrite"
		trap 'rm -f "$temporary_environment" "$temporary_rewrite"' EXIT HUP INT TERM
		cp -p "$legacy_environment" "$temporary_environment"
		sed 's/^YAGOCRAWLER_/YAGO_CRAWLER_/' "$legacy_environment" >"$temporary_rewrite"
		cat "$temporary_rewrite" >"$temporary_environment"
		mv "$temporary_environment" "$canonical_environment"
		rm -f "$temporary_rewrite"
		trap - EXIT HUP INT TERM
	fi
	rm -f "$legacy_environment"
fi

rm -f "$installation_root/opt/yago/bin/yagocrawler"

if [ -d "$installation_root/run/systemd/system" ]; then
	"$systemctl_command" daemon-reload >/dev/null 2>&1 || true
fi
