package yagonode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseNilAwayRatchetRequiresStableDiagnosticFormat(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	scriptPath := filepath.Join(root, "tools", "check-nilaway-ratchet")
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read NilAway ratchet: %v", err)
	}
	temporary := t.TempDir()
	fakeNilAway := writeReleaseNilAwayFake(t, temporary)
	baseline := writeReleaseNilAwayBaseline(t, temporary)

	for _, required := range []string{" -pretty-print=false", " -print-full-file-path=true"} {
		mutated := strings.Replace(string(scriptBytes), required, "", 1)
		if mutated == string(scriptBytes) {
			t.Fatalf("NilAway ratchet is missing %q", required)
		}
		mutatedPath := filepath.Join(temporary, strings.TrimPrefix(required, " -")+"-missing")
		if err := os.WriteFile(mutatedPath, []byte(mutated), 0o700); err != nil {
			t.Fatalf("write mutated NilAway ratchet: %v", err)
		}
		if output, err := runReleaseNilAwayRatchet(
			t,
			mutatedPath,
			fakeNilAway,
			baseline,
			"exact",
		); err == nil {
			t.Fatalf("NilAway ratchet without %q was admitted:\n%s", required, output)
		}
	}
}
