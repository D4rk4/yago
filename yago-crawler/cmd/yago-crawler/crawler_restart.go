package main

import (
	"context"
	"errors"
	"sync/atomic"
)

// restartExitCode is the process exit status for a node-requested crawler
// restart. It is non-zero so every supervisor restart policy brings the worker
// back up — docker restart:always/unless-stopped restart on any exit, but
// systemd Restart=on-failure only restarts a non-zero one.
const restartExitCode = 3

// errRestartRequested marks a run that ended because the node asked the worker to
// restart rather than because of a failure or an operator stop.
var errRestartRequested = errors.New("crawler restart requested")

// restartController turns a restart directive into a graceful shutdown: Trigger
// cancels the run context (in-flight fetches drain under the shutdown grace) and
// Wrap converts the clean shutdown into errRestartRequested so start exits with
// restartExitCode instead of 0.
type restartController struct {
	cancel    context.CancelFunc
	requested atomic.Bool
}

// newRestartController derives the run context from ctx and returns the
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

// Wrap maps a clean shutdown that Trigger caused onto errRestartRequested;
// failures and operator stops pass through unchanged.
func (c *restartController) Wrap(err error) error {
	if err == nil && c.requested.Load() {
		return errRestartRequested
	}

	return err
}
