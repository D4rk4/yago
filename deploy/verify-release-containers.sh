#!/bin/sh
set -eu

version="${1:?release version}"
source_revision="${2:?source revision}"
architecture="${3:?container architecture}"

printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
printf '%s\n' "$source_revision" | grep -Eq '^[0-9a-f]{40}$'
printf '%s\n' "$architecture" | grep -Eq '^(amd64|arm64)$'

node_image="yago-node:${version}"
crawler_image="yago-crawler:${version}"

DOCKER_BUILDKIT=1 docker build \
	--platform "linux/${architecture}" \
	--provenance=false \
	--build-arg "VERSION=${version}" \
	--build-arg "SOURCE_REVISION=${source_revision}" \
	-f yagonode/Dockerfile \
	-t "$node_image" \
	.

DOCKER_BUILDKIT=1 docker build \
	--platform "linux/${architecture}" \
	--provenance=false \
	--build-arg "VERSION=${version}" \
	--build-arg "SOURCE_REVISION=${source_revision}" \
	-f yago-crawler/Dockerfile \
	-t "$crawler_image" \
	.

test "$(docker run --rm "$node_image" --version)" = "yago-node ${version}"
test "$(docker run --rm "$crawler_image" --version)" = "yago-crawler ${version}"
docker run --rm --entrypoint /usr/bin/firefox-esr "$crawler_image" --version >/dev/null 2>&1

node_notice_container=""
node_notice_directory=$(mktemp -d)
remove_node_notice_audit() {
	if [ -n "$node_notice_container" ]; then
		docker rm -f "$node_notice_container" >/dev/null 2>&1 || true
	fi
	rm -rf "$node_notice_directory"
}
trap remove_node_notice_audit EXIT
node_notice_container=$(docker create "$node_image")
docker cp \
	"$node_notice_container:/usr/share/doc/yago/CJK_DICTIONARY_NOTICES.txt" \
	"$node_notice_directory/CJK_DICTIONARY_NOTICES.txt"
docker rm "$node_notice_container" >/dev/null
node_notice_container=""
cmp yagonode/internal/searchindex/CJK_DICTIONARY_NOTICES.txt \
	"$node_notice_directory/CJK_DICTIONARY_NOTICES.txt"
remove_node_notice_audit
trap - EXIT

for image in "$node_image" "$crawler_image"; do
	test "$(docker image inspect --format '{{ .Architecture }}' "$image")" = "$architecture"
	test "$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$image")" = "$source_revision"
	test "$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.source" }}' "$image")" = "https://github.com/D4rk4/yago"
	docker run --rm \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v trivy-cache:/root/.cache \
		aquasec/trivy:0.72.0 image \
		--image-src docker \
		--scanners vuln,secret,misconfig \
		--image-config-scanners secret,misconfig \
		--exit-code 1 \
		--severity HIGH,CRITICAL \
		"$image"
done
