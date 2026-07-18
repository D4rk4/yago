#!/bin/sh
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
temporary_directory=$(mktemp -d)
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM
installation_root="$temporary_directory/root"
systemctl_log="$temporary_directory/systemctl.log"
fake_systemctl="$temporary_directory/systemctl"

cat >"$fake_systemctl" <<'SYSTEMCTL'
#!/bin/sh
printf '%s\n' "$*" >>"$SYSTEMCTL_LOG"
SYSTEMCTL
chmod 755 "$fake_systemctl"

install -d \
	"$installation_root/opt/yago/etc" \
	"$installation_root/opt/yago/bin" \
	"$installation_root/etc/systemd/system" \
	"$installation_root/etc/systemd/system/multi-user.target.wants" \
	"$installation_root/lib/systemd/system" \
	"$installation_root/usr/lib/systemd/system" \
	"$installation_root/run/systemd/system"
printf '%s\n' \
	'YAGOCRAWLER_WORKERS=17' \
	'YAGO_CRAWLER_PRIORITIZE_AUTOMATIC_DISCOVERY=false' \
	'YAGO_DATA_DIR=/srv/yago' \
	>"$installation_root/opt/yago/etc/yagocrawler.env"
touch \
	"$installation_root/opt/yago/bin/yagocrawler" \
	"$installation_root/etc/systemd/system/yagocrawler.service" \
	"$installation_root/lib/systemd/system/yagocrawler.service" \
	"$installation_root/usr/lib/systemd/system/yagocrawler.service"
ln -s ../yagocrawler.service \
	"$installation_root/etc/systemd/system/multi-user.target.wants/yagocrawler.service"

SYSTEMCTL_LOG="$systemctl_log" sh "$here/migrate-crawler-installation.sh" \
	--root "$installation_root" "$fake_systemctl"

test ! -e "$installation_root/opt/yago/etc/yagocrawler.env"
test ! -e "$installation_root/opt/yago/bin/yagocrawler"
test ! -e "$installation_root/etc/systemd/system/yagocrawler.service"
test ! -L "$installation_root/etc/systemd/system/multi-user.target.wants/yagocrawler.service"
test ! -e "$installation_root/lib/systemd/system/yagocrawler.service"
test ! -e "$installation_root/usr/lib/systemd/system/yagocrawler.service"
grep -qx 'YAGO_CRAWLER_WORKERS=17' "$installation_root/opt/yago/etc/yago-crawler.env"
grep -qx 'YAGO_CRAWLER_PRIORITIZE_AUTOMATIC_DISCOVERY=false' "$installation_root/opt/yago/etc/yago-crawler.env"
grep -qx 'YAGO_DATA_DIR=/srv/yago' "$installation_root/opt/yago/etc/yago-crawler.env"
! grep -q '^YAGOCRAWLER_' "$installation_root/opt/yago/etc/yago-crawler.env"
grep -qx 'stop yago-crawler.service yagocrawler.service' "$systemctl_log"
grep -qx 'disable yagocrawler.service' "$systemctl_log"
grep -qx 'daemon-reload' "$systemctl_log"

printf '%s\n' 'YAGO_CRAWLER_WORKERS=31' \
	>"$installation_root/opt/yago/etc/yago-crawler.env"
cp "$installation_root/opt/yago/etc/yago-crawler.env" \
	"$temporary_directory/canonical-before.env"
printf '%s\n' 'YAGOCRAWLER_WORKERS=99' \
	>"$installation_root/opt/yago/etc/yagocrawler.env"

SYSTEMCTL_LOG="$systemctl_log" sh "$here/migrate-crawler-installation.sh" \
	--root "$installation_root" "$fake_systemctl"

cmp "$temporary_directory/canonical-before.env" \
	"$installation_root/opt/yago/etc/yago-crawler.env"
test ! -e "$installation_root/opt/yago/etc/yagocrawler.env"

SYSTEMCTL_LOG="$systemctl_log" sh "$here/migrate-crawler-installation.sh" \
	--root "$installation_root" "$fake_systemctl"
cmp "$temporary_directory/canonical-before.env" \
	"$installation_root/opt/yago/etc/yago-crawler.env"
