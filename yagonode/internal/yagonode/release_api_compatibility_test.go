package yagonode

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseAPICompatibilityAcceptsCompatibleAndRejectsBreakingChange(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	temporary := t.TempDir()
	fakeAPIDiff := filepath.Join(temporary, "apidiff")
	fakeSource := `#!/bin/sh
if [ "$1" = "-m" ] && [ "$2" = "-w" ]; then
	printf 'synthetic export\n' > "$3"
	exit 0
fi
if [ "${APIDIFF_FAKE_MODE:-compatible}" = "incompatible" ]; then
	printf 'Removed: SyntheticAPI\n'
fi
`
	if err := os.WriteFile(fakeAPIDiff, []byte(fakeSource), 0o755); err != nil {
		t.Fatalf("write fake apidiff: %v", err)
	}
	script := filepath.Join(root, "tools", "check-api-compatibility")
	baseline := filepath.Join(root, "tools", "api-compatibility-baseline")

	if output, err := runReleaseAPICompatibility(
		t,
		script,
		fakeAPIDiff,
		baseline,
		"compatible",
	); err != nil {
		t.Fatalf("compatible API was refused: %v\n%s", err, output)
	}
	if output, err := runReleaseAPICompatibility(
		t,
		script,
		fakeAPIDiff,
		baseline,
		"incompatible",
	); err == nil {
		t.Fatalf("incompatible API was admitted:\n%s", output)
	}

	wrongBaseline := filepath.Join(temporary, "wrong-baseline")
	if err := os.WriteFile(
		wrongBaseline,
		[]byte("v0.0.56 0000000000000000000000000000000000000000\n"),
		0o600,
	); err != nil {
		t.Fatalf("write invalid API baseline: %v", err)
	}
	if output, err := runReleaseAPICompatibility(
		t,
		script,
		fakeAPIDiff,
		wrongBaseline,
		"compatible",
	); err == nil {
		t.Fatalf("moved API baseline was admitted:\n%s", output)
	}
}

func TestReleaseAPICompatibilityBaselinePinsPublishedIdentity(t *testing.T) {
	contents, err := os.ReadFile("../../../tools/api-compatibility-baseline")
	if err != nil {
		t.Fatalf("read API compatibility baseline: %v", err)
	}
	if got := strings.TrimSpace(string(contents)); got !=
		"v0.0.56 b7472e93de54309a6da36ef307ff9140952ba950" {
		t.Fatalf("API compatibility baseline = %q", got)
	}
}

func runReleaseAPICompatibility(
	t *testing.T,
	script string,
	fakeAPIDiff string,
	baseline string,
	mode string,
) (string, error) {
	t.Helper()
	command := exec.CommandContext(
		t.Context(),
		"/bin/sh",
		script,
		fakeAPIDiff,
		baseline,
		"yagomodel",
	)
	command.Env = append(os.Environ(), "APIDIFF_FAKE_MODE="+mode)
	output, err := command.CombinedOutput()
	return string(output), err
}
