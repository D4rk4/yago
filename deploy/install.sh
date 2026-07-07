#!/bin/sh
# Bare-metal installer: creates the /opt/yago layout, the yago system user,
# and installs the systemd units (OPS-04). The Debian package's postinst
# performs these same steps; this script serves hosts installing from a
# release tarball. Idempotent — safe to re-run for upgrades.
#
# Usage: install.sh <dir-with-binaries> [deploy-dir]
#   <dir-with-binaries>  contains yago-node and yagocrawler
#   [deploy-dir]         contains systemd/ (defaults to this script's dir)
set -eu

bindir="${1:?usage: install.sh <dir-with-binaries> [deploy-dir]}"
deploydir="${2:-$(dirname "$0")}"

for binary in yago-node yagocrawler; do
	[ -f "$bindir/$binary" ] || { echo "missing $bindir/$binary" >&2; exit 66; }
done

if ! getent passwd yago >/dev/null; then
	useradd --system --home-dir /opt/yago/data --shell /usr/sbin/nologin \
		--user-group yago
fi

install -d -m 755 /opt/yago/bin /opt/yago/etc
install -d -m 750 -o yago -g yago /opt/yago/data /opt/yago/data/crawler
install -m 755 "$bindir/yago-node" "$bindir/yagocrawler" /opt/yago/bin/

for env in yago-node yagocrawler; do
	if [ ! -f "/opt/yago/etc/$env.env" ]; then
		install -m 640 -g yago "$deploydir/systemd/$env.env.example" \
			"/opt/yago/etc/$env.env"
	fi
done

install -m 644 "$deploydir/systemd/yago-node.service" \
	"$deploydir/systemd/yagocrawler.service" /etc/systemd/system/

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload
	systemctl enable yago-node.service yagocrawler.service
	echo "installed; edit /opt/yago/etc/*.env then: systemctl start yago-node yagocrawler"
else
	echo "installed; systemd not running — units placed in /etc/systemd/system"
fi
