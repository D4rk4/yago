#!/bin/sh
# Build the yago .deb (OPS-05). Runs anywhere dpkg-deb exists — CI uses a
# Debian container. The package installs into /opt/yago, creates the yago
# system user, and enables the systemd units; purge keeps /opt/yago/data.
#
# Usage: build-deb.sh <version> <arch> <dir-with-binaries> <output-dir>
set -eu

version="${1:?version (e.g. 1.2.3)}"
arch="${2:?dpkg arch (amd64|arm64)}"
bindir="${3:?dir with yago-node + yagocrawler}"
outdir="${4:?output dir}"

root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

install -d "$root/opt/yago/bin" "$root/opt/yago/etc" "$root/lib/systemd/system" \
	"$root/usr/share/doc/yago" "$root/DEBIAN"
install -m 755 "$bindir/yago-node" "$bindir/yagocrawler" "$root/opt/yago/bin/"
install -m 644 "$here/../systemd/yago-node.env.example" \
	"$here/../systemd/yagocrawler.env.example" "$root/opt/yago/etc/"
install -m 644 "$here/../systemd/yago-node.service" \
	"$here/../systemd/yagocrawler.service" "$root/lib/systemd/system/"
install -m 644 "$here/../../doc/backup-restore.md" "$root/usr/share/doc/yago/"

cat > "$root/DEBIAN/control" <<CONTROL
Package: yago
Version: $version
Architecture: $arch
Maintainer: YagoSeek <noreply@github.com/D4rk4/yago>
Depends: ca-certificates, adduser
Recommends: chromium | chromium-browser
Section: net
Priority: optional
Homepage: https://github.com/D4rk4/yago
Description: YagoSeek peer-to-peer search engine node and crawler
 A modern Go implementation of a YaCy-compatible peer-to-peer search
 engine: the node serves search and swarm protocol traffic, the crawler
 fetches and parses pages. Data lives in /opt/yago/data and survives
 package purge.
CONTROL

cat > "$root/DEBIAN/postinst" <<'POSTINST'
#!/bin/sh
set -eu
if ! getent passwd yago >/dev/null; then
	adduser --system --group --home /opt/yago/data \
		--no-create-home --quiet yago
fi
install -d -m 750 -o yago -g yago /opt/yago/data /opt/yago/data/crawler
for env in yago-node yagocrawler; do
	if [ ! -f "/opt/yago/etc/$env.env" ]; then
		install -m 640 -g yago "/opt/yago/etc/$env.env.example" \
			"/opt/yago/etc/$env.env"
	fi
done
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload
	systemctl enable yago-node.service yagocrawler.service >/dev/null 2>&1 || true
fi
POSTINST

cat > "$root/DEBIAN/prerm" <<'PRERM'
#!/bin/sh
set -eu
if [ -d /run/systemd/system ]; then
	systemctl stop yagocrawler.service yago-node.service 2>/dev/null || true
	systemctl disable yagocrawler.service yago-node.service >/dev/null 2>&1 || true
fi
PRERM

cat > "$root/DEBIAN/postrm" <<'POSTRM'
#!/bin/sh
set -eu
# Data outlives the package on purpose: /opt/yago/data holds the operator's
# index and the node identity. Purge removes the edited env files only.
if [ "$1" = "purge" ]; then
	rm -f /opt/yago/etc/yago-node.env /opt/yago/etc/yagocrawler.env
fi
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
fi
POSTRM

chmod 755 "$root/DEBIAN/postinst" "$root/DEBIAN/prerm" "$root/DEBIAN/postrm"
mkdir -p "$outdir"
dpkg-deb --build --root-owner-group "$root" \
	"$outdir/yago_${version}_${arch}.deb"
