package yagonode

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const releaseContainerRegistryFailureShell = `
docker() {
	if test "$1" = load; then
		case "$3" in
		*arm64*) architecture=arm64 ;;
		*) architecture=amd64 ;;
		esac
		return 0
	fi
	if test "$1" = image && test "$2" = inspect; then
		case "$4" in
		*Architecture*) printf '%s\n' "$architecture" ;;
		*revision*) printf '%s\n' "$SOURCE_REVISION" ;;
		*source*) printf 'https://github.com/D4rk4/yago\n' ;;
		*Id*) printf 'sha256:validated-image\n' ;;
		*) return 91 ;;
		esac
		return 0
	fi
	if test "$1" = buildx && test "$2" = imagetools && test "$3" = inspect; then
		printf '503 Service Unavailable\n' >&2
		return 1
	fi
	printf 'unexpected registry mutation or Docker command: %s\n' "$*" >&2
	return 92
}
set -- v0.0.10 "$SOURCE_REVISION" "$ARCHIVE_DIRECTORY"
. ../../../deploy/publish-release-containers.sh
`

const releaseContainerMatchingRegistryShell = `
docker() {
	if test "$1" = load; then
		case "$3" in
		*arm64*) architecture=arm64 ;;
		*) architecture=amd64 ;;
		esac
		return 0
	fi
	if test "$1" = image && test "$2" = inspect; then
		case "$4" in
		*Architecture*) printf '%s\n' "$architecture" ;;
		*revision*) printf '%s\n' "$SOURCE_REVISION" ;;
		*source*) printf 'https://github.com/D4rk4/yago\n' ;;
		*Id*)
			case "$5" in
			*yago-crawler*|*yagocrawler*) printf 'sha256:crawler-%s\n' "$architecture" ;;
			*) printf 'sha256:node-%s\n' "$architecture" ;;
			esac
			;;
		*) return 81 ;;
		esac
		return 0
	fi
	if test "$1" = pull; then
		return 0
	fi
	if test "$1" = buildx && test "$2" = imagetools && test "$3" = inspect; then
		if test "${5-}" != --format; then
			return 0
		fi
		reference="$4"
		format="$6"
		case "$format" in
		*Manifest.Manifests*)
			case "$reference" in
			*yago-node*)
				printf '%s linux amd64 \n' "$NODE_AMD64_DIGEST"
				printf '%s linux arm64 \n' "$NODE_ARM64_DIGEST"
				;;
			*yagocrawler*)
				printf '%s linux amd64 \n' "$CRAWLER_AMD64_DIGEST"
				printf '%s linux arm64 \n' "$CRAWLER_ARM64_DIGEST"
				;;
			esac
			;;
		*Manifest.MediaType*) printf 'application/vnd.docker.distribution.manifest.list.v2+json\n' ;;
		*Manifest.Digest*)
			case "$reference" in
			*yago-node:v0.0.10-amd64) printf '%s\n' "$NODE_AMD64_DIGEST" ;;
			*yago-node:v0.0.10-arm64) printf '%s\n' "$NODE_ARM64_DIGEST" ;;
			*yagocrawler:v0.0.10-amd64) printf '%s\n' "$CRAWLER_AMD64_DIGEST" ;;
			*yagocrawler:v0.0.10-arm64) printf '%s\n' "$CRAWLER_ARM64_DIGEST" ;;
			*yago-node:v0.0.10) printf '%s\n' "$NODE_DIGEST" ;;
			*yagocrawler:v0.0.10) printf '%s\n' "$CRAWLER_DIGEST" ;;
			*) return 82 ;;
			esac
			;;
		*) return 83 ;;
		esac
		return 0
	fi
	printf 'unexpected registry mutation or Docker command: %s\n' "$*" >&2
	return 84
}
set -- v0.0.10 "$SOURCE_REVISION" "$ARCHIVE_DIRECTORY"
. ../../../deploy/publish-release-containers.sh
`

func TestReleaseWorkflowHasOnlyTagPushTrigger(t *testing.T) {
	contents, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		"on:\n  push:\n    tags: [\"v*\"]",
		`RELEASE_TAG: ${{ github.ref_name }}`,
		`RELEASE_REF: ${{ github.ref }}`,
		`RELEASE_SOURCE_SHA: ${{ github.sha }}`,
		`test "$GITHUB_REF" = "$RELEASE_REF"`,
		`test "$GITHUB_SHA" = "$RELEASE_SOURCE_SHA"`,
		`ref: ${{ env.RELEASE_SOURCE_SHA }}`,
		`needs: [build, containers]`,
		`group: release-container-publish-${{ github.ref }}`,
		`needs: [build, container_publish]`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("release tag workflow guard missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`workflow_dispatch`,
		`types: [edited]`,
		`BACKFILL_`,
		`RELEASE_TAG_OBJECT`,
		`historical`,
		`v0.0.10`,
		`355175485`,
		`8384841304`,
		`actions: read`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("release workflow retains temporary trigger evidence %q", forbidden)
		}
	}
}

func TestReleaseContainerPublicationAttestsExactVersionManifests(t *testing.T) {
	contents, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	text := string(contents)
	publishStart := strings.Index(text, "\n  container_publish:\n")
	if publishStart < 0 {
		t.Fatal("release workflow does not define registry publication")
	}
	releaseOffset := strings.Index(text[publishStart:], "\n  release:\n")
	if releaseOffset < 0 {
		t.Fatal(
			"release workflow does not place GitHub Release publication after registry publication",
		)
	}
	releaseStart := publishStart + releaseOffset
	publishJob := text[publishStart:releaseStart]
	for _, required := range []string{
		`packages: write`,
		`id-token: write`,
		`attestations: write`,
		`pattern: release-containers-*`,
		`docker login ghcr.io`,
		`publish-release-containers.sh`,
		`subject-name: ghcr.io/d4rk4/yago-node`,
		`subject-name: ghcr.io/d4rk4/yagocrawler`,
		`push-to-registry: true`,
		`create-storage-record: false`,
		`--bundle-from-oci`,
		`WORKFLOW_SHA: ${{ github.workflow_sha }}`,
		`--signer-digest "$WORKFLOW_SHA"`,
		`--source-ref "$GITHUB_REF"`,
		`--source-digest "$GITHUB_SHA"`,
		`--deny-self-hosted-runners`,
		`cancel-in-progress: false`,
		`Verify public release containers without credentials`,
		`verify-public-release-containers.sh`,
	} {
		if !strings.Contains(publishJob, required) {
			t.Fatalf("release registry publication missing %q", required)
		}
	}
	if strings.Contains(publishJob, `:latest`) || strings.Contains(publishJob, `secrets.CR_PAT`) {
		t.Fatal("release registry publication uses a moving tag or long-lived registry token")
	}
	if strings.Index(publishJob, `Publish multi-architecture release containers`) >
		strings.Index(publishJob, `Attest node container manifest`) {
		t.Fatal("release workflow attests a container manifest before publication")
	}
	if strings.Index(publishJob, `Attest crawler container manifest`) >
		strings.Index(publishJob, `Verify container manifest attestations`) {
		t.Fatal("release workflow verifies container attestations before creating both")
	}
	if strings.Index(publishJob, `Verify container manifest attestations`) >
		strings.Index(publishJob, `Verify public release containers without credentials`) {
		t.Fatal("release workflow checks public access before verifying attestations")
	}
	if !strings.Contains(text[releaseStart:], `pattern: dist-*`) {
		t.Fatal("GitHub Release download can include internal container archives")
	}
}

func TestReleaseV0010RecordsCompletedContainerBackfill(t *testing.T) {
	contents, err := os.ReadFile("../../../doc/releases/v0.0.10.md")
	if err != nil {
		t.Fatalf("read release memo: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		`### Factual correction — 2026-07-16`,
		`29525608480`,
		`fc40ab5bb62de50b060a82874919c49ad429652f`,
		`09ca7be1b1e5065155111479c9213bd0566801d8`,
		`9bcc0bde61364c8248fba7f452c19f2446c72898`,
		`sha256:b50c94fb17c498bbb185785efac9e03e9505384f7abb95ab8555f16a62d9f445`,
		`sha256:82fc9336f3b7e2bf662ad978451df0410de08c64819ec6281d724580e58165cd`,
		`sha256:ff5472dc9a2e9c936ee528ea7d3f743d811e66c8325b92f5935347f5306fe51c`,
		`sha256:c3e8a2553456da6b1788b344d5f48660ae51d7d9b35166f18fbd815423ed1669`,
		`sha256:cb0c8f216e4773d87da79a0e75885233a88abcc917c93a0d38064d9895d10802`,
		`sha256:f5608032651e8e71e3979b4710822f8717afd6c6b823441200557a872477cfa4`,
		`https://slsa.dev/provenance/v1`,
		`doc/release-container-identity-v1.md`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("release correction missing %q", required)
		}
	}
	schema, err := os.ReadFile("../../../doc/release-container-identity-v1.md")
	if err != nil {
		t.Fatalf("read historical identity schema: %v", err)
	}
	for _, required := range []string{
		`# Release container identity v1`,
		"`schemaVersion`: integer `1`",
		"`release`:",
		"`workflow`:",
		"`validation`:",
		"`manifests`:",
	} {
		if !strings.Contains(string(schema), required) {
			t.Fatalf("historical identity schema missing %q", required)
		}
	}
	identityType := "https://github.com/D4rk4/yago/blob/" + strings.Repeat(
		"0",
		40,
	) + "/doc/release-container-identity-v1.md"
	if len(identityType) > 128 {
		t.Fatalf("historical identity predicate type length = %d", len(identityType))
	}
}

func TestReleaseContainerPromotionUsesValidatedArchivesWithoutRebuild(t *testing.T) {
	exportContents, err := os.ReadFile("../../../deploy/export-release-containers.sh")
	if err != nil {
		t.Fatalf("read release container export: %v", err)
	}
	publishContents, err := os.ReadFile("../../../deploy/publish-release-containers.sh")
	if err != nil {
		t.Fatalf("read release container publication: %v", err)
	}
	exportText := string(exportContents)
	publishText := string(publishContents)
	for _, required := range []string{
		`docker save --output`,
		`sha256sum "$archive_name"`,
		`org.opencontainers.image.revision`,
		`org.opencontainers.image.source`,
	} {
		if !strings.Contains(exportText, required) {
			t.Fatalf("release container export missing %q", required)
		}
	}
	for _, required := range []string{
		`sha256sum -c`,
		`docker load --input`,
		`docker push`,
		`ghcr.io/d4rk4/yago-node`,
		`ghcr.io/d4rk4/yagocrawler`,
		`docker buildx imagetools create`,
		`release_reference_state`,
		`cannot determine release container reference state`,
		`expected_image_identity`,
		`actual_descriptors`,
		`application/vnd.docker.distribution.manifest.list.v2+json`,
	} {
		if !strings.Contains(publishText, required) {
			t.Fatalf("release container publication missing %q", required)
		}
	}
	if strings.Contains(publishText, `docker build `) || strings.Contains(publishText, `:latest`) {
		t.Fatal("release container publication rebuilds an image or creates a moving tag")
	}
	if strings.Contains(publishText, `--annotation`) ||
		strings.Contains(publishText, `oci.image.index`) {
		t.Fatal(
			"release container publication claims OCI root metadata for Docker exporter manifests",
		)
	}
	if strings.Contains(exportText, `docker push`) || strings.Contains(exportText, `docker login`) {
		t.Fatal("native validation job exports directly to a registry")
	}
}

func TestPublicReleaseContainerVerificationUsesEmptyCredentials(t *testing.T) {
	contents, err := os.ReadFile("../../../deploy/verify-public-release-containers.sh")
	if err != nil {
		t.Fatalf("read public release container verification: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		`anonymous_docker_configuration=$(mktemp -d)`,
		`DOCKER_CONFIG="$anonymous_docker_configuration" docker buildx imagetools inspect "$tagged_reference"`,
		`DOCKER_CONFIG="$anonymous_docker_configuration" docker buildx imagetools inspect "$digest_reference"`,
		`DOCKER_CONFIG="$anonymous_docker_configuration" docker pull --platform linux/amd64 "$tagged_reference"`,
		`DOCKER_CONFIG="$anonymous_docker_configuration" docker pull --platform linux/amd64 "$digest_reference"`,
		`org.opencontainers.image.revision`,
		`org.opencontainers.image.source`,
		`docker run --rm "$digest_reference" --version`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("public release container verification missing %q", required)
		}
	}
	if strings.Contains(text, `docker login`) {
		t.Fatal("public release container verification authenticates to the registry")
	}
}

func TestReleaseContainerPublicationFailsClosedOnRegistryFailure(t *testing.T) {
	archiveDirectory := t.TempDir()
	writeEmptyReleaseContainerArchives(t, archiveDirectory)
	sourceRevision := "9bcc0bde61364c8248fba7f452c19f2446c72898"
	command := releaseContainerRegistryFailureTestCommand(t)
	command.Env = append(command.Env,
		"SOURCE_REVISION="+sourceRevision,
		"ARCHIVE_DIRECTORY="+archiveDirectory,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("release publication accepted registry failure: %s", output)
	}
	if !strings.Contains(string(output), "503 Service Unavailable") {
		t.Fatalf("release publication hid registry failure: %s", output)
	}
}

func TestReleaseContainerPublicationResumesMatchingRegistryState(t *testing.T) {
	archiveDirectory := t.TempDir()
	writeEmptyReleaseContainerArchives(t, archiveDirectory)
	sourceRevision := "9bcc0bde61364c8248fba7f452c19f2446c72898"
	nodeAMD64Digest := "sha256:" + strings.Repeat("a", 64)
	nodeARM64Digest := "sha256:" + strings.Repeat("b", 64)
	crawlerAMD64Digest := "sha256:" + strings.Repeat("c", 64)
	crawlerARM64Digest := "sha256:" + strings.Repeat("d", 64)
	nodeDigest := "sha256:" + strings.Repeat("e", 64)
	crawlerDigest := "sha256:" + strings.Repeat("f", 64)
	command := releaseContainerMatchingRegistryTestCommand(t)
	command.Env = append(command.Env,
		"SOURCE_REVISION="+sourceRevision,
		"ARCHIVE_DIRECTORY="+archiveDirectory,
		"NODE_AMD64_DIGEST="+nodeAMD64Digest,
		"NODE_ARM64_DIGEST="+nodeARM64Digest,
		"CRAWLER_AMD64_DIGEST="+crawlerAMD64Digest,
		"CRAWLER_ARM64_DIGEST="+crawlerARM64Digest,
		"NODE_DIGEST="+nodeDigest,
		"CRAWLER_DIGEST="+crawlerDigest,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("resume matching release publication: %v\n%s", err, output)
	}
	for _, expected := range []string{
		"node_digest=" + nodeDigest,
		"crawler_digest=" + crawlerDigest,
		"node_amd64_digest=" + nodeAMD64Digest,
		"node_arm64_digest=" + nodeARM64Digest,
		"crawler_amd64_digest=" + crawlerAMD64Digest,
		"crawler_arm64_digest=" + crawlerARM64Digest,
	} {
		if !strings.Contains(string(output), expected) {
			t.Fatalf("resumed release publication missing %q: %s", expected, output)
		}
	}
}

func releaseContainerRegistryFailureTestCommand(t *testing.T) *exec.Cmd {
	t.Helper()
	commandContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(
		commandContext,
		"/bin/sh",
		"-c",
		releaseContainerRegistryFailureShell,
	)
	command.Env = os.Environ()
	return command
}

func releaseContainerMatchingRegistryTestCommand(t *testing.T) *exec.Cmd {
	t.Helper()
	commandContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(
		commandContext,
		"/bin/sh",
		"-c",
		releaseContainerMatchingRegistryShell,
	)
	command.Env = os.Environ()
	return command
}

func writeEmptyReleaseContainerArchives(t *testing.T, directory string) {
	t.Helper()
	emptyArchiveDigest := sha256.Sum256(nil)
	for _, architecture := range []string{"amd64", "arm64"} {
		archiveName := "release-containers-" + architecture + ".tar"
		if err := os.WriteFile(filepath.Join(directory, archiveName), nil, 0o600); err != nil {
			t.Fatalf("write empty release container archive: %v", err)
		}
		checksum := fmt.Sprintf("%x  %s\n", emptyArchiveDigest, archiveName)
		if err := os.WriteFile(
			filepath.Join(directory, archiveName+".sha256"),
			[]byte(checksum),
			0o600,
		); err != nil {
			t.Fatalf("write release container archive checksum: %v", err)
		}
	}
}
