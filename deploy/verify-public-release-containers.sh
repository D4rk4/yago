#!/bin/sh
set -eu

version="${1:?release version}"
source_revision="${2:?source revision}"
node_digest="${3:?node manifest digest}"
crawler_digest="${4:?crawler manifest digest}"

printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
printf '%s\n' "$source_revision" | grep -Eq '^[0-9a-f]{40}$'
for digest in "$node_digest" "$crawler_digest"; do
	printf '%s\n' "$digest" | grep -Eq '^sha256:[0-9a-f]{64}$'
done

anonymous_docker_configuration=$(mktemp -d)
trap 'rm -rf "$anonymous_docker_configuration"' EXIT HUP INT TERM

verify_public_release_container() {
	repository="$1"
	digest="$2"
	expected_version="$3"
	tagged_reference="${repository}:${version}"
	digest_reference="${repository}@${digest}"
	tagged_digest=$(DOCKER_CONFIG="$anonymous_docker_configuration" docker buildx imagetools inspect "$tagged_reference" --format '{{ .Manifest.Digest }}')
	test "$tagged_digest" = "$digest"
	DOCKER_CONFIG="$anonymous_docker_configuration" docker buildx imagetools inspect "$digest_reference" >/dev/null
	DOCKER_CONFIG="$anonymous_docker_configuration" docker pull --platform linux/amd64 "$tagged_reference" >/dev/null
	tagged_image_identity=$(docker image inspect --format '{{ .Id }}' "$tagged_reference")
	DOCKER_CONFIG="$anonymous_docker_configuration" docker pull --platform linux/amd64 "$digest_reference" >/dev/null
	test "$(docker image inspect --format '{{ .Id }}' "$digest_reference")" = "$tagged_image_identity"
	test "$(docker image inspect --format '{{ .Architecture }}' "$digest_reference")" = amd64
	test "$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$digest_reference")" = "$source_revision"
	test "$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.source" }}' "$digest_reference")" = "https://github.com/D4rk4/yago"
	test "$(docker run --rm "$digest_reference" --version)" = "$expected_version"
}

verify_public_release_container ghcr.io/d4rk4/yago-node "$node_digest" "yago-node ${version}"
verify_public_release_container ghcr.io/d4rk4/yago-crawler "$crawler_digest" "yago-crawler ${version}"
