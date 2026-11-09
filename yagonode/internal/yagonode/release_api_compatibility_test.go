package yagonode

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type releaseAPICompatibilityFixture struct {
	script        string
	fakeAPIDiff   string
	baseline      string
	configuredTag string
	temporary     string
}

func newReleaseAPICompatibilityFixture(t *testing.T) releaseAPICompatibilityFixture {
	t.Helper()
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
	baseline := filepath.Join(root, "tools", "api-compatibility-baseline")
	contents, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatalf("read configured API baseline: %v", err)
	}

	return releaseAPICompatibilityFixture{
		script:        filepath.Join(root, "tools", "check-api-compatibility"),
		fakeAPIDiff:   fakeAPIDiff,
		baseline:      baseline,
		configuredTag: strings.Fields(string(contents))[0],
		temporary:     temporary,
	}
}

func (f releaseAPICompatibilityFixture) exceptions(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(f.temporary, "exceptions")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write API compatibility exceptions: %v", err)
	}

	return path
}

func (f releaseAPICompatibilityFixture) run(
	t *testing.T,
	baseline string,
	exceptions string,
	mode string,
) (string, error) {
	t.Helper()
	command := exec.CommandContext(
		t.Context(),
		"/bin/sh",
		f.script,
		f.fakeAPIDiff,
		baseline,
		exceptions,
		"yagomodel",
	)
	command.Env = append(os.Environ(), "APIDIFF_FAKE_MODE="+mode)
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestReleaseAPICompatibilityAcceptsCompatibleAndRejectsBreakingChange(t *testing.T) {
	fixture := newReleaseAPICompatibilityFixture(t)
	noExceptions := fixture.exceptions(t, "")

	if output, err := fixture.run(t, fixture.baseline, noExceptions, "compatible"); err != nil {
		t.Fatalf("compatible API was refused: %v\n%s", err, output)
	}
	if output, err := fixture.run(t, fixture.baseline, noExceptions, "incompatible"); err == nil {
		t.Fatalf("incompatible API was admitted:\n%s", output)
	}

	wrongBaseline := filepath.Join(fixture.temporary, "wrong-baseline")
	if err := os.WriteFile(
		wrongBaseline,
		[]byte(fixture.configuredTag+" 0000000000000000000000000000000000000000\n"),
		0o600,
	); err != nil {
		t.Fatalf("write invalid API baseline: %v", err)
	}
	if output, err := fixture.run(t, wrongBaseline, noExceptions, "compatible"); err == nil {
		t.Fatalf("moved API baseline was admitted:\n%s", output)
	}
}

func TestReleaseAPICompatibilityExceptionsArePinnedToTheBaseline(t *testing.T) {
	fixture := newReleaseAPICompatibilityFixture(t)
	tag := fixture.configuredTag
	for _, test := range []struct {
		name       string
		exceptions string
		mode       string
		admitted   bool
	}{
		{"recorded change", tag + " yagomodel Removed: SyntheticAPI\n", "incompatible", true},
		{"other module", tag + " yagoproto Removed: SyntheticAPI\n", "incompatible", false},
		{"other change", tag + " yagomodel Removed: OtherAPI\n", "incompatible", false},
		{"unused exception", tag + " yagomodel Removed: SyntheticAPI\n", "compatible", false},
		{"stale baseline", "v0.0.0 yagomodel Removed: SyntheticAPI\n", "incompatible", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := fixture.run(
				t,
				fixture.baseline,
				fixture.exceptions(t, test.exceptions),
				test.mode,
			)
			if (err == nil) != test.admitted {
				t.Fatalf("admitted = %t, want %t: %v\n%s", err == nil, test.admitted, err, output)
			}
		})
	}
}
