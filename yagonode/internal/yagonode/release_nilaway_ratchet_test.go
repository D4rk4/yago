package yagonode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestReleaseNilAwayRatchetAcceptsExactAndRejectsChangedDiagnostics(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	temporary := t.TempDir()
	fakeNilAway := filepath.Join(temporary, "nilaway")
	fakeSource := `#!/bin/sh
case "${NILAWAY_FAKE_MODE:-exact}" in
exact)
	printf '%s\n' "$PWD/internal/sample.go:1:2: Potential nil panic detected. synthetic"
	exit 3
	;;
extra)
	printf '%s\n' "$PWD/internal/sample.go:1:2: Potential nil panic detected. synthetic"
	printf '%s\n' "$PWD/internal/new.go:3:4: Potential nil panic detected. synthetic"
	exit 3
	;;
clean)
	exit 0
	;;
unclassified)
	printf 'synthetic diagnostic\n'
	exit 3
	;;
failure)
	exit 2
	;;
esac
`
	if err := os.WriteFile(fakeNilAway, []byte(fakeSource), 0o755); err != nil {
		t.Fatalf("write fake NilAway: %v", err)
	}
	baseline := filepath.Join(temporary, "baseline")
	if err := os.WriteFile(
		baseline,
		[]byte("yagomodel/internal/sample.go:1:2\n"),
		0o600,
	); err != nil {
		t.Fatalf("write NilAway baseline: %v", err)
	}
	script := filepath.Join(root, "tools", "check-nilaway-ratchet")

	if output, err := runReleaseNilAwayRatchet(
		t,
		script,
		fakeNilAway,
		baseline,
		"exact",
	); err != nil {
		t.Fatalf("exact NilAway baseline was refused: %v\n%s", err, output)
	}
	for _, mode := range []string{"extra", "clean", "unclassified", "failure"} {
		if output, err := runReleaseNilAwayRatchet(
			t,
			script,
			fakeNilAway,
			baseline,
			mode,
		); err == nil {
			t.Fatalf("NilAway mode %q was admitted:\n%s", mode, output)
		}
	}
}

func TestReleaseNilAwayBaselineContainsOnlyExactSortedLocations(t *testing.T) {
	contents, err := os.ReadFile("../../../tools/nilaway-baseline")
	if err != nil {
		t.Fatalf("read NilAway baseline: %v", err)
	}
	locations := strings.Fields(string(contents))
	if len(locations) != 82 {
		t.Fatalf("NilAway baseline locations = %d, want 82", len(locations))
	}
	if err := validateReleaseNilAwayLocations(locations); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]string{
		{"yagonode/internal/b.go:2:1", "yagonode/internal/a.go:1:1"},
		{"yagonode/internal/a.go:1:1", "yagonode/internal/a.go:1:1"},
		{"yagonode/internal"},
	} {
		if err := validateReleaseNilAwayLocations(invalid); err == nil {
			t.Fatalf("invalid NilAway locations were admitted: %v", invalid)
		}
	}
}

func validateReleaseNilAwayLocations(locations []string) error {
	wantOrder := slices.Clone(locations)
	slices.Sort(wantOrder)
	if !slices.Equal(locations, wantOrder) {
		return fmt.Errorf("NilAway baseline is not sorted")
	}
	exactLocation := regexp.MustCompile(
		`^(?:yagonode|yago-crawler)/.+\.go:[1-9][0-9]*:[1-9][0-9]*$`,
	)
	for index, location := range locations {
		if !exactLocation.MatchString(location) {
			return fmt.Errorf("NilAway baseline contains non-exact location %q", location)
		}
		if index > 0 && locations[index-1] == location {
			return fmt.Errorf("NilAway baseline duplicates %q", location)
		}
	}
	return nil
}

func runReleaseNilAwayRatchet(
	t *testing.T,
	script string,
	fakeNilAway string,
	baseline string,
	mode string,
) (string, error) {
	t.Helper()
	command := exec.CommandContext(
		t.Context(),
		"/bin/sh",
		script,
		fakeNilAway,
		baseline,
		"yagomodel",
	)
	command.Env = append(os.Environ(), "NILAWAY_FAKE_MODE="+mode)
	output, err := command.CombinedOutput()
	return string(output), err
}
