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
