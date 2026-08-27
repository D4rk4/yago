package yagonode

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const releaseContainerPushShell = `
docker() {
	test "$1" = push
	attempts=0
	if test -f "$PUSH_ATTEMPTS_FILE"; then
		attempts=$(cat "$PUSH_ATTEMPTS_FILE")
	fi
	attempts=$((attempts + 1))
	printf '%s\n' "$attempts" >"$PUSH_ATTEMPTS_FILE"
	case "$PUSH_MODE" in
	success)
		printf 'pushed\n'
		;;
	recover)
		if test "$attempts" -eq 1; then
			printf 'unknown blob\n' >&2
			return 1
		fi
		printf 'pushed\n'
		;;
	persistent)
		printf 'blob unknown to registry\n' >&2
		return 1
		;;
	authorization)
		printf 'denied: permission_denied\n' >&2
		return 1
		;;
	*)
		return 92
		;;
	esac
}
sleep() {
	printf '%s\n' "$1" >>"$PUSH_DELAYS_FILE"
}
set -- ghcr.io/d4rk4/yago-node:v0.0.47-arm64
. ../../../deploy/push-release-container.sh
`

type releaseContainerPushResult struct {
	output   string
	err      error
	attempts string
	delays   string
}

func TestReleaseContainerPushLeavesImmediateSuccessUnchanged(t *testing.T) {
	result := executeReleaseContainerPush(t, "success")
	if result.err != nil {
		t.Fatalf("push release container: %v\n%s", result.err, result.output)
	}
	if result.attempts != "1" || result.delays != "" {
		t.Fatalf("immediate push attempts = %q, delays = %q", result.attempts, result.delays)
	}
}

func TestReleaseContainerPushRetriesRegistryBlobRefusal(t *testing.T) {
	result := executeReleaseContainerPush(t, "recover")
	if result.err != nil {
		t.Fatalf("retry release container push: %v\n%s", result.err, result.output)
	}
	if result.attempts != "2" || result.delays != "2" {
		t.Fatalf("recovered push attempts = %q, delays = %q", result.attempts, result.delays)
	}
	if !strings.Contains(result.output, "reason=registry_blob_unavailable") {
		t.Fatalf("recovered push output = %q", result.output)
	}
}

func TestReleaseContainerPushBoundsPersistentRegistryBlobRefusal(t *testing.T) {
	result := executeReleaseContainerPush(t, "persistent")
	if result.err == nil {
		t.Fatalf("persistent blob refusal accepted: %s", result.output)
	}
	if result.attempts != "3" || result.delays != "2\n4" {
		t.Fatalf("persistent push attempts = %q, delays = %q", result.attempts, result.delays)
	}
}

func TestReleaseContainerPushRefusesUnrelatedRegistryFailureWithoutRetry(t *testing.T) {
	result := executeReleaseContainerPush(t, "authorization")
	if result.err == nil {
		t.Fatalf("authorization refusal accepted: %s", result.output)
	}
	if result.attempts != "1" || result.delays != "" {
		t.Fatalf("authorization attempts = %q, delays = %q", result.attempts, result.delays)
	}
}

func executeReleaseContainerPush(t *testing.T, mode string) releaseContainerPushResult {
	t.Helper()
	temporaryDirectory := t.TempDir()
	attemptsPath := filepath.Join(temporaryDirectory, "attempts")
	delaysPath := filepath.Join(temporaryDirectory, "delays")
	commandContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(
		commandContext,
		"/bin/sh",
		"-c",
		releaseContainerPushShell,
	)
	command.Env = releaseContainerPushEnvironment(attemptsPath, delaysPath, mode)
	output, commandError := command.CombinedOutput()
	return releaseContainerPushResult{
		output:   string(output),
		err:      commandError,
		attempts: readReleaseContainerPushResult(t, temporaryDirectory, "attempts"),
		delays:   readReleaseContainerPushResult(t, temporaryDirectory, "delays"),
	}
}

func releaseContainerPushEnvironment(
	attemptsPath string,
	delaysPath string,
	mode string,
) []string {
	environment := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "PUSH_ATTEMPTS_FILE=") ||
			strings.HasPrefix(entry, "PUSH_DELAYS_FILE=") ||
			strings.HasPrefix(entry, "PUSH_MODE=") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(
		environment,
		"PUSH_ATTEMPTS_FILE="+attemptsPath,
		"PUSH_DELAYS_FILE="+delaysPath,
		"PUSH_MODE="+mode,
	)
}

func readReleaseContainerPushResult(t *testing.T, directory string, name string) string {
	t.Helper()
	directoryRoot, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("open push result directory: %v", err)
	}
	t.Cleanup(func() {
		if closeError := directoryRoot.Close(); closeError != nil {
			t.Errorf("close push result directory: %v", closeError)
		}
	})
	contents, err := directoryRoot.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatalf("read push result %s: %v", name, err)
	}
	return strings.TrimSpace(string(contents))
}
