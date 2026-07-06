package yagonode

import (
	"context"
	"errors"
	"sync/atomic"
)

// restartExitCode is the process exit status for an operator-requested
// restart. It is non-zero so every supervisor restart policy brings the node
// back up — docker restart:always/unless-stopped restart on any exit, but
// systemd Restart=on-failure only restarts a non-zero one.
const restartExitCode = 3

// errRestartRequested marks a serve loop that ended because a restart was
// requested rather than because of a failure or an operator stop.
var errRestartRequested = errors.New("node restart requested")

// restartController turns a restart request into a graceful shutdown: Trigger
// cancels the serve context (in-flight HTTP responses still complete under the
// shutdown timeout) and Wrap converts the clean serve result into
// errRestartRequested so Main exits with restartExitCode instead of 0.
type restartController struct {
	cancel    context.CancelFunc
	requested atomic.Bool
}

// newRestartController derives the serve context from ctx and returns the
// controller governing it.
func newRestartController(ctx context.Context) (context.Context, *restartController) {
	ctx, cancel := context.WithCancel(ctx)

	return ctx, &restartController{cancel: cancel}
}

// Trigger requests the restart; safe to call more than once.
func (c *restartController) Trigger() {
	c.requested.Store(true)
	c.cancel()
}

// Wrap maps a clean shutdown that was caused by Trigger onto
// errRestartRequested; failures pass through unchanged.
func (c *restartController) Wrap(err error) error {
	if err == nil && c.requested.Load() {
		return errRestartRequested
	}

	return err
}
