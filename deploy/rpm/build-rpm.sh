#!/bin/sh
# Build the yago .rpm (OPS-05, the RPM sibling of ../debian/build-deb.sh). Runs
# anywhere rpmbuild exists — CI installs the `rpm` package on the Ubuntu runner.
# The package mirrors the .deb: it installs into /opt/yago, creates the yago
# system user, and enables the systemd units; removal keeps /opt/yago/data.
#
# The binaries are static (CGO off), so rpmbuild only packages prebuilt files:
# debuginfo extraction and the strip/build-id post-processing are disabled
# (they would run the host toolchain against a cross-arch binary and fail), and
# AutoReqProv is off since a static binary pulls no shared libraries.
#
# Usage: build-rpm.sh <version> <arch> <dir-with-binaries> <output-dir>
#   <arch> is the Go/deb arch (amd64|arm64); it maps to the RPM arch
#   (x86_64|aarch64) so one release matrix drives both package builders.
set -eu

version="${1:?version (e.g. 1.2.3)}"
arch="${2:?arch (amd64|arm64)}"
bindir="${3:?dir with yago-node + yagocrawler}"
outdir="${4:?output dir}"

case "$arch" in
	amd64) rpmarch=x86_64 ;;
	arm64) rpmarch=aarch64 ;;
	*) echo "unsupported arch: $arch (want amd64|arm64)" >&2; exit 2 ;;
esac

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
deploy=$(CDPATH= cd -- "$here/.." && pwd)
repo=$(CDPATH= cd -- "$deploy/.." && pwd)
bindir=$(CDPATH= cd -- "$bindir" && pwd)
mkdir -p "$outdir"
outdir=$(CDPATH= cd -- "$outdir" && pwd)

top=$(mktemp -d)
trap 'rm -rf "$top"' EXIT
spec="$top/yago.spec"

cat > "$spec" <<'SPEC'
%global debug_package %{nil}
%global __os_install_post %{nil}
%define _unitdir /usr/lib/systemd/system

Name:           yago
Version:        %{yver}
Release:        1
Summary:        YagoSeek peer-to-peer search engine node and crawler
License:        AGPL-3.0-or-later
URL:            https://github.com/D4rk4/yago
AutoReqProv:    no
Requires:       ca-certificates
Requires(pre):  shadow-utils
Recommends:     firefox

%description
A modern Go implementation of a YaCy-compatible peer-to-peer search engine:
the node serves search and swarm protocol traffic, the crawler fetches and
parses pages. Data lives in /opt/yago/data and survives package removal.

%install
rm -rf %{buildroot}
install -d -m 755 %{buildroot}/opt/yago/bin %{buildroot}/opt/yago/etc
install -d -m 755 %{buildroot}%{_unitdir} %{buildroot}/usr/share/doc/yago
install -m 755 %{ybin}/yago-node %{ybin}/yagocrawler %{buildroot}/opt/yago/bin/
install -m 644 %{ydeploy}/systemd/yago-node.env.example \
	%{ydeploy}/systemd/yagocrawler.env.example %{buildroot}/opt/yago/etc/
install -m 644 %{ydeploy}/systemd/yago-node.service \
	%{ydeploy}/systemd/yagocrawler.service %{buildroot}%{_unitdir}/
install -m 644 %{yrepo}/doc/backup-restore.md %{buildroot}/usr/share/doc/yago/

%files
%dir /opt/yago
%dir /opt/yago/bin
%dir /opt/yago/etc
/opt/yago/bin/yago-node
/opt/yago/bin/yagocrawler
/opt/yago/etc/yago-node.env.example
/opt/yago/etc/yagocrawler.env.example
%{_unitdir}/yago-node.service
%{_unitdir}/yagocrawler.service
%dir /usr/share/doc/yago
/usr/share/doc/yago/backup-restore.md

%pre
getent group yago >/dev/null || groupadd -r yago
getent passwd yago >/dev/null || \
	useradd -r -g yago -d /opt/yago/data -s /sbin/nologin -M yago

%post
install -d -m 750 -o yago -g yago /opt/yago/data /opt/yago/data/crawler
for env in yago-node yagocrawler; do
	if [ ! -f "/opt/yago/etc/$env.env" ]; then
		install -m 640 -g yago "/opt/yago/etc/$env.env.example" \
			"/opt/yago/etc/$env.env"
	fi
done
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
	systemctl enable yago-node.service yagocrawler.service >/dev/null 2>&1 || true
fi

%preun
# $1 is the count of this package left after the operation: 0 means a real
# uninstall (not an upgrade), when the units should be stopped and disabled.
if [ "$1" -eq 0 ] && [ -d /run/systemd/system ]; then
	systemctl stop yagocrawler.service yago-node.service 2>/dev/null || true
	systemctl disable yagocrawler.service yago-node.service >/dev/null 2>&1 || true
fi

%postun
# /opt/yago/data (index + node identity) and any edited env files are kept on
# purpose; rpm removes only the files it installed. Reload systemd if present.
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
fi
SPEC

rpmbuild -bb \
	--target "$rpmarch" \
	--define "_topdir $top" \
	--define "_rpmdir $top/RPMS" \
	--define "_build_id_links none" \
	--define "yver $version" \
	--define "ybin $bindir" \
	--define "ydeploy $deploy" \
	--define "yrepo $repo" \
	"$spec"

mv "$top/RPMS/$rpmarch/yago-${version}-1.${rpmarch}.rpm" "$outdir/"
echo "built $outdir/yago-${version}-1.${rpmarch}.rpm"
