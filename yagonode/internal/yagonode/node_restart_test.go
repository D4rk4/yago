package yagonode

import (
	"context"
	"errors"
	"testing"
)

func TestRestartControllerTriggerCancelsAndWrapsCleanShutdown(t *testing.T) {
	ctx, restart := newRestartController(context.Background())
	if err := restart.Wrap(nil); err != nil {
		t.Fatalf("untriggered clean shutdown wrapped: %v", err)
	}

	restart.Trigger()
	restart.Trigger()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("trigger did not cancel the serve context")
	}
	if err := restart.Wrap(nil); !errors.Is(err, errRestartRequested) {
		t.Fatalf("Wrap(nil) = %v, want errRestartRequested", err)
	}

	failure := errors.New("listener exploded")
	if err := restart.Wrap(failure); !errors.Is(err, failure) {
		t.Fatalf("Wrap must pass failures through, got %v", err)
	}
}

func TestMainExitsWithRestartCodeOnRequestedRestart(t *testing.T) {
	oldRun, oldExit := runNode, exitProcess
	defer func() { runNode, exitProcess = oldRun, oldExit }()

	exitCode := -1
	runNode = func() error { return errRestartRequested }
	exitProcess = func(code int) { exitCode = code }
	Main()
	if exitCode != restartExitCode {
		t.Fatalf("exit code = %d, want %d", exitCode, restartExitCode)
	}
}
