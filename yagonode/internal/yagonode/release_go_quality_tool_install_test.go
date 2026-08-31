package yagonode

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type releaseGoQualityToolInstallerFixture struct {
	installer string
	fakeGo    string
	bin       string
	lock      string
}

func TestReleaseGoQualityToolInstallerVerifiesEmbeddedModuleIdentity(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	temporary := t.TempDir()
	bin := filepath.Join(temporary, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatalf("create synthetic tool directory: %v", err)
	}
	lock := filepath.Join(temporary, "lock")
	if err := os.WriteFile(
		lock,
		[]byte("synthetic example.com/tool v1.2.3 example.com/tool/cmd/synthetic\n"),
		0o600,
	); err != nil {
		t.Fatalf("write synthetic tool lock: %v", err)
	}
	fakeGo := filepath.Join(temporary, "go")
	fakeSource := `#!/bin/sh
if [ "$1" = "install" ]; then
	printf 'synthetic binary\n' > "$GOBIN/synthetic"
	chmod 0755 "$GOBIN/synthetic"
	exit 0
fi
if [ "${GO_QUALITY_FAKE_MODE:-match}" = "match" ]; then
	printf 'mod\texample.com/tool\tv1.2.3\n'
else
	printf 'mod\texample.com/tool\tv9.9.9\n'
fi
`
	if err := os.WriteFile(fakeGo, []byte(fakeSource), 0o755); err != nil {
		t.Fatalf("write fake Go command: %v", err)
	}
	installer := filepath.Join(root, "tools", "install-go-quality")
	fixture := releaseGoQualityToolInstallerFixture{
		installer: installer,
		fakeGo:    fakeGo,
		bin:       bin,
		lock:      lock,
	}

	if output, err := runReleaseGoQualityToolInstaller(
		t,
		fixture,
		"match",
	); err != nil {
		t.Fatalf("matching tool identity was refused: %v\n%s", err, output)
	}
	if output, err := runReleaseGoQualityToolInstaller(
		t,
		fixture,
		"mismatch",
	); err == nil {
		t.Fatalf("mismatched tool identity was admitted:\n%s", output)
	}

	malformed := filepath.Join(temporary, "malformed-lock")
	if err := os.WriteFile(malformed, []byte("synthetic\n"), 0o600); err != nil {
		t.Fatalf("write malformed tool lock: %v", err)
	}
	fixture.lock = malformed
	if output, err := runReleaseGoQualityToolInstaller(
		t,
		fixture,
		"match",
	); err == nil {
		t.Fatalf("malformed tool lock was admitted:\n%s", output)
	}
}

func runReleaseGoQualityToolInstaller(
	t *testing.T,
	fixture releaseGoQualityToolInstallerFixture,
	mode string,
) (string, error) {
	t.Helper()
	command := exec.CommandContext(
		t.Context(),
		"/bin/sh",
		fixture.installer,
		fixture.bin,
		fixture.lock,
	)
	command.Env = append(
		os.Environ(),
		"GO="+fixture.fakeGo,
		"GO_QUALITY_FAKE_MODE="+mode,
	)
	output, err := command.CombinedOutput()
	return string(output), err
}
