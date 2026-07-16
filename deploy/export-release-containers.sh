#!/bin/sh
set -eu

version="${1:?release version}"
source_revision="${2:?source revision}"
architecture="${3:?container architecture}"
output_directory="${4:?output directory}"

printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
printf '%s\n' "$source_revision" | grep -Eq '^[0-9a-f]{40}$'
printf '%s\n' "$architecture" | grep -Eq '^(amd64|arm64)$'

node_image="yago-node:${version}"
crawler_image="yago-crawler:${version}"

for image in "$node_image" "$crawler_image"; do
	test "$(docker image inspect --format '{{ .Architecture }}' "$image")" = "$architecture"
	test "$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$image")" = "$source_revision"
	test "$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.source" }}' "$image")" = "https://github.com/D4rk4/yago"
done

mkdir -p "$output_directory"
archive_name="release-containers-${architecture}.tar"
archive="$output_directory/$archive_name"
test ! -e "$archive"
docker save --output "$archive" "$node_image" "$crawler_image"
(
	cd "$output_directory"
	sha256sum "$archive_name" >"$archive_name.sha256"
)
