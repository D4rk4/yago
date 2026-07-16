#!/bin/sh
set -eu

version="${1:?release version}"
source_revision="${2:?source revision}"
archive_directory="${3:?archive directory}"

printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
printf '%s\n' "$source_revision" | grep -Eq '^[0-9a-f]{40}$'
test -d "$archive_directory"

node_repository="ghcr.io/d4rk4/yago-node"
crawler_repository="ghcr.io/d4rk4/yagocrawler"

release_reference_state() {
	reference="$1"
	if inspection=$(docker buildx imagetools inspect "$reference" 2>&1); then
		printf 'present'
		return
	fi
	if printf '%s\n' "$inspection" | grep -Eiq '(manifest unknown|name unknown|manifest[^[:cntrl:]]*not found|repository[^[:cntrl:]]*not found|: not found([[:space:]]|$))'; then
		printf 'missing'
		return
	fi
	printf 'cannot determine release container reference state for %s:\n%s\n' "$reference" "$inspection" >&2
	return 1
}

release_child_digest() {
	local_image="$1"
	release_reference="$2"
	architecture="$3"
	expected_image_identity=$(docker image inspect --format '{{ .Id }}' "$local_image")
	reference_state=$(release_reference_state "$release_reference")
	if test "$reference_state" = missing; then
		docker tag "$local_image" "$release_reference"
		docker push "$release_reference" >&2
	fi
	docker pull --platform "linux/${architecture}" "$release_reference" >&2
	test "$(docker image inspect --format '{{ .Id }}' "$release_reference")" = "$expected_image_identity"
	test "$(docker image inspect --format '{{ .Architecture }}' "$release_reference")" = "$architecture"
	test "$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$release_reference")" = "$source_revision"
	test "$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.source" }}' "$release_reference")" = "https://github.com/D4rk4/yago"
	child_digest=$(docker buildx imagetools inspect "$release_reference" --format '{{ .Manifest.Digest }}')
	printf '%s\n' "$child_digest" | grep -Eq '^sha256:[0-9a-f]{64}$'
	printf '%s' "$child_digest"
}

release_manifest_digest() {
	release_reference="$1"
	repository="$2"
	amd64_digest="$3"
	arm64_digest="$4"
	reference_state=$(release_reference_state "$release_reference")
	if test "$reference_state" = missing; then
		docker buildx imagetools create \
			--tag "$release_reference" \
			"${repository}@${amd64_digest}" \
			"${repository}@${arm64_digest}" >&2
	fi
	expected_descriptors=$(printf '%s linux/amd64\n%s linux/arm64\n' "$amd64_digest" "$arm64_digest" | sort)
	actual_descriptors=$(docker buildx imagetools inspect "$release_reference" --format '{{ range .Manifest.Manifests }}{{ println .Digest .Platform.OS .Platform.Architecture .Platform.Variant }}{{ end }}' |
		awk 'NF == 3 { print $1 " " $2 "/" $3 } NF == 4 { print $1 " " $2 "/" $3 "/" $4 }' |
		sort)
	test "$actual_descriptors" = "$expected_descriptors"
	media_type=$(docker buildx imagetools inspect "$release_reference" --format '{{ .Manifest.MediaType }}')
	test "$media_type" = application/vnd.docker.distribution.manifest.list.v2+json
	digest=$(docker buildx imagetools inspect "$release_reference" --format '{{ .Manifest.Digest }}')
	printf '%s\n' "$digest" | grep -Eq '^sha256:[0-9a-f]{64}$'
	printf '%s' "$digest"
}

for architecture in amd64 arm64; do
	archive_name="release-containers-${architecture}.tar"
	(
		cd "$archive_directory"
		sha256sum -c "$archive_name.sha256"
	) >&2
	docker load --input "$archive_directory/$archive_name" >&2

	node_image="yago-node:${version}"
	crawler_image="yago-crawler:${version}"
	for image in "$node_image" "$crawler_image"; do
		test "$(docker image inspect --format '{{ .Architecture }}' "$image")" = "$architecture"
		test "$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$image")" = "$source_revision"
		test "$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.source" }}' "$image")" = "https://github.com/D4rk4/yago"
	done

	node_reference="${node_repository}:${version}-${architecture}"
	crawler_reference="${crawler_repository}:${version}-${architecture}"
	node_child_digest=$(release_child_digest "$node_image" "$node_reference" "$architecture")
	crawler_child_digest=$(release_child_digest "$crawler_image" "$crawler_reference" "$architecture")

	case "$architecture" in
	amd64)
		node_amd64_digest="$node_child_digest"
		crawler_amd64_digest="$crawler_child_digest"
		;;
	arm64)
		node_arm64_digest="$node_child_digest"
		crawler_arm64_digest="$crawler_child_digest"
		;;
	esac
done

node_reference="${node_repository}:${version}"
crawler_reference="${crawler_repository}:${version}"

node_digest=$(release_manifest_digest "$node_reference" "$node_repository" "$node_amd64_digest" "$node_arm64_digest")
crawler_digest=$(release_manifest_digest "$crawler_reference" "$crawler_repository" "$crawler_amd64_digest" "$crawler_arm64_digest")

printf 'node_digest=%s\n' "$node_digest"
printf 'crawler_digest=%s\n' "$crawler_digest"
printf 'node_amd64_digest=%s\n' "$node_amd64_digest"
printf 'node_arm64_digest=%s\n' "$node_arm64_digest"
printf 'crawler_amd64_digest=%s\n' "$crawler_amd64_digest"
printf 'crawler_arm64_digest=%s\n' "$crawler_arm64_digest"
