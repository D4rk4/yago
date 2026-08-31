package yagonode

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const releaseFuzzSmokeGoCommand = `#!/bin/sh
if [ "$1" = "list" ]; then
	if [ "${FUZZ_FAKE_MODE:-pass}" = "empty" ]; then
		printf '%s|example.test|ordinary_test.go|\n' "$FUZZ_FAKE_DIR"
	else
		printf '%s|example.test|fuzz_test.go|\n' "$FUZZ_FAKE_DIR"
	fi
	exit 0
fi
if [ "$1" = "test" ] && [ "$2" = "-list" ]; then
	printf 'FuzzSynthetic\n'
	exit 0
fi
printf '%s\n' "$*" >> "$FUZZ_FAKE_LOG"
if [ "${FUZZ_FAKE_MODE:-pass}" = "fail" ]; then
	exit 1
fi
`

type releaseFuzzSmokeFixture struct {
	script          string
	fakeGo          string
	targetDirectory string
	invocations     string
}

func TestReleaseFuzzSmokeIsBoundedAndRejectsMissingOrFailingTargets(t *testing.T) {
	fixture := newReleaseFuzzSmokeFixture(t)
	requireReleaseFuzzSmokePass(t, fixture)
	requireReleaseFuzzSmokeRefusals(t, fixture)
}

func newReleaseFuzzSmokeFixture(t *testing.T) releaseFuzzSmokeFixture {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	temporary := t.TempDir()
	fuzzTest := filepath.Join(temporary, "fuzz_test.go")
	if err := os.WriteFile(fuzzTest, []byte("func FuzzSynthetic"), 0o600); err != nil {
		t.Fatalf("write synthetic fuzz target: %v", err)
	}
	ordinaryTest := filepath.Join(temporary, "ordinary_test.go")
	if err := os.WriteFile(ordinaryTest, []byte("func TestSynthetic"), 0o600); err != nil {
		t.Fatalf("write synthetic ordinary target: %v", err)
	}
	fakeGo := filepath.Join(temporary, "go")
	if err := os.WriteFile(fakeGo, []byte(releaseFuzzSmokeGoCommand), 0o755); err != nil {
		t.Fatalf("write fake Go command: %v", err)
	}
	return releaseFuzzSmokeFixture{
		script:          filepath.Join(root, "tools", "run-fuzz-smoke"),
		fakeGo:          fakeGo,
		targetDirectory: temporary,
		invocations:     filepath.Join(temporary, "invocations"),
	}
}

func requireReleaseFuzzSmokePass(t *testing.T, fixture releaseFuzzSmokeFixture) {
	t.Helper()
	if output, err := runReleaseFuzzSmoke(t, fixture, "pass", "5s"); err != nil {
		t.Fatalf("bounded fuzz target was refused: %v\n%s", err, output)
	}
	logged, err := os.ReadFile(fixture.invocations)
	if err != nil {
		t.Fatalf("read fuzz invocation: %v", err)
	}
	for _, required := range []string{
		"-run=^$",
		"-fuzz=^FuzzSynthetic$",
		"-fuzztime=5s",
		"-parallel=1",
	} {
		if !strings.Contains(string(logged), required) {
			t.Fatalf("fuzz invocation missing %q: %s", required, logged)
		}
	}
}

func requireReleaseFuzzSmokeRefusals(t *testing.T, fixture releaseFuzzSmokeFixture) {
	t.Helper()
	for _, testCase := range []struct {
		mode     string
		duration string
	}{
		{mode: "empty", duration: "5s"},
		{mode: "fail", duration: "5s"},
		{mode: "pass", duration: "0s"},
		{mode: "pass", duration: "31s"},
		{mode: "pass", duration: "unbounded"},
	} {
		if output, err := runReleaseFuzzSmoke(
			t,
			fixture,
			testCase.mode,
			testCase.duration,
		); err == nil {
			t.Fatalf(
				"fuzz mode %q duration %q was admitted:\n%s",
				testCase.mode,
				testCase.duration,
				output,
			)
		}
	}
}

func runReleaseFuzzSmoke(
	t *testing.T,
	fixture releaseFuzzSmokeFixture,
	mode string,
	duration string,
) (string, error) {
	t.Helper()
	command := exec.CommandContext(
		t.Context(),
		"/bin/sh",
		fixture.script,
		fixture.fakeGo,
		duration,
		"yagomodel",
	)
	command.Env = append(
		os.Environ(),
		"FUZZ_FAKE_DIR="+fixture.targetDirectory,
		"FUZZ_FAKE_LOG="+fixture.invocations,
		"FUZZ_FAKE_MODE="+mode,
	)
	output, err := command.CombinedOutput()
	return string(output), err
}
