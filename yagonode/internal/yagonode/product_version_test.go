package yagonode

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestProductVersionUsesReleaseOrDatedDevelopmentIdentity(t *testing.T) {
	pattern := regexp.MustCompile(`^(?:v[0-9]+\.[0-9]+\.[0-9]+|[0-9]{4}\.[0-9]{2}\.[0-9]{2}-dev)$`)
	if !pattern.MatchString(Version()) {
		t.Fatalf("Version() = %q, want vN.N.N or YYYY.MM.DD-dev", Version())
	}
}

func TestContainerProductVersionRequiresCleanExactTag(t *testing.T) {
	repository := t.TempDir()
	runVersionCommand(t, repository, exec.CommandContext(t.Context(), "git", "init", "-q"))
	runVersionCommand(
		t,
		repository,
		exec.CommandContext(t.Context(), "git", "config", "user.name", "YaGo Test"),
	)
	runVersionCommand(
		t,
		repository,
		exec.CommandContext(t.Context(), "git", "config", "user.email", "yago@example.invalid"),
	)
	tracked := filepath.Join(repository, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("released\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runVersionCommand(
		t,
		repository,
		exec.CommandContext(t.Context(), "git", "add", "tracked.txt"),
	)
	runVersionCommand(
		t,
		repository,
		exec.CommandContext(t.Context(), "git", "commit", "-q", "-m", "release fixture"),
	)
	runVersionCommand(t, repository, exec.CommandContext(t.Context(), "git", "tag", "v1.2.3"))

	if got := runProductVersion(t, repository); got != "v1.2.3" {
		t.Fatalf("clean tag version = %q", got)
	}

	if err := os.WriteFile(tracked, []byte("modified\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertDevelopmentVersion(t, runProductVersion(t, repository))

	if err := os.WriteFile(tracked, []byte("released\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repository, "untracked.txt"),
		[]byte("new\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	assertDevelopmentVersion(t, runProductVersion(t, repository))
}

func runVersionCommand(t *testing.T, directory string, command *exec.Cmd) {
	t.Helper()
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v: %s", command.String(), err, output)
	}
}

func runProductVersion(t *testing.T, directory string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "sh", "../../../tools/product-version")
	command.Env = append(
		os.Environ(),
		"GIT_DIR="+filepath.Join(directory, ".git"),
		"GIT_WORK_TREE="+directory,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("product version: %v: %s", err, output)
	}

	return strings.TrimSpace(string(output))
}

func assertDevelopmentVersion(t *testing.T, value string) {
	t.Helper()
	if !regexp.MustCompile(`^[0-9]{4}\.[0-9]{2}\.[0-9]{2}-dev$`).MatchString(value) {
		t.Fatalf("development version = %q", value)
	}
}

func TestNodeContainerRequiresCallerStampedBuildIdentity(t *testing.T) {
	contents, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		`ARG VERSION=`,
		`set -eu;`,
		`[0-9]{4}\.[0-9]{2}\.[0-9]{2}-dev`,
		`buildVersion=${VERSION}`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Dockerfile missing %q", required)
		}
	}
	if strings.Contains(text, `date -u`) {
		t.Fatal("Dockerfile derives a cacheable build date internally")
	}
}

func TestReleaseWorkflowRequiresExactSemanticTag(t *testing.T) {
	contents, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		`grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'`,
		`version="${GITHUB_REF_NAME}"`,
		`yago-node v$2`,
		`yago-crawler v$2`,
		`id-token: write`,
		`attestations: write`,
		`artifact-metadata: write`,
		`Attest release artifacts`,
		`actions/attest@a1948c3f048ba23858d222213b7c278aabede763`,
		`dist/*.tar.gz`,
		`dist/*.deb`,
		`dist/*.rpm`,
		`Verify artifact attestations`,
		`--signer-workflow "$GITHUB_REPOSITORY/.github/workflows/release.yml"`,
		`--source-ref "$GITHUB_REF"`,
		`--source-digest "$GITHUB_SHA"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("release workflow missing %q", required)
		}
	}
	if strings.Index(text, `Attest release artifacts`) > strings.Index(text, `Upload artifacts`) {
		t.Fatal("release workflow uploads artifacts before attesting them")
	}
	if strings.Index(text, `Verify artifact attestations`) >
		strings.Index(text, `Create GitHub Release`) {
		t.Fatal("release workflow publishes artifacts before verifying attestations")
	}
}

func TestReleaseContainerWorkflowSeparatesValidationFromRegistryPublication(t *testing.T) {
	contents, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	text := string(contents)
	containerStart := strings.Index(text, "\n  containers:\n")
	containerPublishStart := strings.Index(text, "\n  container_publish:\n")
	if containerStart < 0 || containerPublishStart <= containerStart {
		t.Fatal("release workflow does not separate validation and registry publication")
	}
	releaseOffset := strings.Index(text[containerPublishStart:], "\n  release:\n")
	if releaseOffset < 0 {
		t.Fatal("release workflow does not define release publication after registry publication")
	}
	releaseStart := containerPublishStart + releaseOffset
	containerJob := text[containerStart:containerPublishStart]
	for _, required := range []string{
		`needs: verify`,
		`runner: ubuntu-24.04`,
		`runner: ubuntu-24.04-arm`,
		`architecture: amd64`,
		`architecture: arm64`,
		`sh deploy/verify-release-containers.sh "$GITHUB_REF_NAME" "$GITHUB_SHA"`,
		`sh .release-workflow/deploy/export-release-containers.sh`,
		`name: release-containers-${{ matrix.architecture }}`,
	} {
		if !strings.Contains(containerJob, required) {
			t.Fatalf("release container job missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`docker push`,
		`docker login`,
		`--push`,
		`actions/attest`,
		`docker manifest`,
		`skopeo copy`,
		`oras push`,
		`type=registry`,
	} {
		if strings.Contains(containerJob, forbidden) {
			t.Fatalf("release container job contains publishing operation %q", forbidden)
		}
	}
	if !strings.Contains(text[releaseStart:], `needs: [build, container_publish]`) {
		t.Fatal("release publication does not depend on registry publication")
	}
}

func TestReleaseContainerBuildGateStaysLocal(t *testing.T) {
	script, err := os.ReadFile("../../../deploy/verify-release-containers.sh")
	if err != nil {
		t.Fatalf("read release container gate: %v", err)
	}
	scriptText := string(script)
	for _, required := range []string{
		`yago-node:${version}`,
		`yago-crawler:${version}`,
		`--platform "linux/${architecture}"`,
		`--provenance=false`,
		`SOURCE_REVISION=${source_revision}`,
		`org.opencontainers.image.revision`,
		`org.opencontainers.image.source`,
		`/usr/bin/firefox-esr`,
		`aquasec/trivy:0.72.0 image`,
		`--image-src docker`,
		`--scanners vuln,secret,misconfig`,
		`--image-config-scanners secret,misconfig`,
		`--exit-code 1`,
		`--severity HIGH,CRITICAL`,
	} {
		if !strings.Contains(scriptText, required) {
			t.Fatalf("release container gate missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`docker push`,
		`docker login`,
		`--push`,
		`upload-artifact`,
		`actions/attest`,
		`docker manifest`,
		`skopeo copy`,
		`oras push`,
		`type=registry`,
	} {
		if strings.Contains(scriptText, forbidden) {
			t.Fatalf("release container gate contains publishing operation %q", forbidden)
		}
	}
	if strings.Count(scriptText, `--provenance=false`) != 2 {
		t.Fatal("release container gate does not disable provenance for both builds")
	}
}

func TestVerifyRejectsUntidyModules(t *testing.T) {
	contents, err := os.ReadFile("../../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		`tidy-check:`,
		`$(GO) mod tidy -diff`,
		`verify: tidy-check`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Makefile missing %q", required)
		}
	}
}

func TestImageMakeTargetRejectsDirtyReleaseIdentity(t *testing.T) {
	contents, err := os.ReadFile("../../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	if !strings.Contains(string(contents), `PRODUCT_VERSION ?= $(shell ./tools/product-version)`) {
		t.Fatal("Makefile does not use the worktree-aware product version command")
	}
}
