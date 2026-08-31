package yagonode

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestReleaseQualityGateRequiresEveryCodeQualityControl(t *testing.T) {
	makefileBytes, err := os.ReadFile("../../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(makefileBytes)
	requireReleaseQualityPrerequisites(t, makefile)
	requireReleaseQualityCommands(t, makefile)
	requireReleaseQualityToolPins(t)
	requireReleaseQualityWorkflow(t)
}

func requireReleaseQualityPrerequisites(t *testing.T, makefile string) {
	t.Helper()
	verifyLine, prerequisites, ok := makeTargetPrerequisites(makefile, "verify")
	if !ok {
		t.Fatal("Makefile verify target is missing")
	}
	for _, required := range []string{
		"gosec",
		"nilaway",
		"api-compatibility",
		"govulncheck",
		"race",
		"test-shuffle",
		"fuzz-smoke",
	} {
		if !slices.Contains(prerequisites, required) {
			t.Fatalf("verify prerequisites = %v, missing %q", prerequisites, required)
		}

		withoutRequired := strings.Replace(verifyLine, " "+required, "", 1)
		mutated := strings.Replace(makefile, verifyLine, withoutRequired, 1)
		_, refusedPrerequisites, found := makeTargetPrerequisites(mutated, "verify")
		if !found || slices.Contains(refusedPrerequisites, required) {
			t.Fatalf("missing %q was not refused: %v", required, refusedPrerequisites)
		}
	}
}

func requireReleaseQualityCommands(t *testing.T, makefile string) {
	t.Helper()
	for _, required := range []string{
		`GOSEC := $(TOOLS_BIN)/gosec`,
		`NILAWAY := $(TOOLS_BIN)/nilaway`,
		`APIDIFF := $(TOOLS_BIN)/apidiff`,
		`GOVULNCHECK := $(TOOLS_BIN)/govulncheck`,
		`$(GOSEC) -quiet -exclude-generated`,
		`-conf=../.gosec.json`,
		`./tools/check-nilaway-ratchet $(NILAWAY) $(NILAWAY_BASELINE) $(MODULES)`,
		`./tools/check-api-compatibility $(APIDIFF) $(API_COMPATIBILITY_BASELINE)`,
		`$(GOVULNCHECK) -scan=symbol ./...`,
		`race:`,
		`$(GO) test -race ./...`,
		`test: race`,
		`$(GO) test -count=1 -shuffle=$(TEST_SHUFFLE_SEED) ./...`,
		`./tools/run-fuzz-smoke $(GO) $(FUZZ_SMOKE_TIME) $(MODULES)`,
	} {
		if !strings.Contains(makefile, required) {
			t.Fatalf("Makefile quality gate missing %q", required)
		}
	}
}

func requireReleaseQualityToolPins(t *testing.T) {
	t.Helper()
	goToolLockBytes, err := os.ReadFile("../../../tools/go-quality-tools.lock")
	if err != nil {
		t.Fatalf("read Go quality tool lock: %v", err)
	}
	goToolLock := string(goToolLockBytes)
	for _, entry := range []string{
		"nilaway go.uber.org/nilaway v0.0.0-20260808063849-8649a03c818a go.uber.org/nilaway/cmd/nilaway",
		"apidiff golang.org/x/exp v0.0.0-20260824195058-e88cd73687aa golang.org/x/exp/cmd/apidiff",
		"govulncheck golang.org/x/vuln v1.7.0 golang.org/x/vuln/cmd/govulncheck",
	} {
		if !strings.Contains(goToolLock, entry) {
			t.Fatalf("Go quality tool lock missing %q", entry)
		}
	}

	toolLockBytes, err := os.ReadFile("../../../tools/tools.lock")
	if err != nil {
		t.Fatalf("read tool lock: %v", err)
	}
	toolLock := string(toolLockBytes)
	for _, platform := range []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64"} {
		if !strings.Contains(toolLock, "gosec 2.28.0 "+platform) {
			t.Fatalf("tool lock missing gosec for %s", platform)
		}
	}

	installerBytes, err := os.ReadFile("../../../tools/install")
	if err != nil {
		t.Fatalf("read tool installer: %v", err)
	}
	if !strings.Contains(
		string(installerBytes),
		`https://github.com/securego/gosec/releases/download/v$ver/gosec_${ver}_${os}_${arch}.tar.gz`,
	) {
		t.Fatal("tool installer does not fetch the pinned official gosec archive")
	}
	if !strings.Contains(string(installerBytes), `"$root/tools/install-go-quality"`) {
		t.Fatal("tool installer does not install the pinned Go quality tools")
	}
}

func requireReleaseQualityWorkflow(t *testing.T) {
	t.Helper()
	workflowBytes, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	if !strings.Contains(
		string(workflowBytes),
		"      - name: Verify source and blocking code-quality gates\n        run: make verify",
	) {
		t.Fatal("release workflow does not expose the blocking code-quality gate")
	}
	for _, required := range []string{
		"            .toolchain/bin",
		"${{ runner.arch }}",
		"hashFiles('**/go.sum', 'tools/*.lock', 'tools/install*')",
	} {
		if !strings.Contains(string(workflowBytes), required) {
			t.Fatalf("release workflow tool cache missing %q", required)
		}
	}
}

func makeTargetPrerequisites(source, target string) (string, []string, bool) {
	prefix := target + ":"
	for line := range strings.SplitSeq(source, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line, strings.Fields(strings.TrimPrefix(line, prefix)), true
		}
	}

	return "", nil, false
}
