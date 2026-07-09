// Package servergroup runs a set of HTTP servers alongside background workers
// until the context is cancelled or any server or worker returns, then shuts
// every server down gracefully.
package servergroup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
)

type NamedServer struct {
	Name   string
	Server *http.Server
}

// Run serves every server and runs every worker until ctx is cancelled or any
// server or worker returns. The first return then triggers a graceful shutdown
// of every server within shutdownTimeout. Run returns the first non-nil error;
// http.ErrServerClosed from a shutdown server is not an error.
func Run(
	ctx context.Context,
	shutdownTimeout time.Duration,
	servers []NamedServer,
	workers ...func(context.Context) error,
) error {
	group, groupCtx := errgroup.WithContext(ctx)
	runCtx, cancel := context.WithCancel(groupCtx)
	defer cancel()

	for _, s := range servers {
		group.Go(func() error {
			defer cancel()
			if err := s.Server.ListenAndServe(); err != nil &&
				!errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("serve %s: %w", s.Name, err)
			}
			return nil
		})
	}

	for _, worker := range workers {
		group.Go(func() error {
			defer cancel()
			return worker(runCtx)
		})
	}

	group.Go(func() error {
		<-runCtx.Done()
		return shutdown(servers, shutdownTimeout)
	})

	return group.Wait()
}

func shutdown(servers []NamedServer, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var failures error
	for _, s := range servers {
		if err := s.Server.Shutdown(shutdownCtx); err != nil {
			failures = errors.Join(failures, fmt.Errorf("shutdown %s: %w", s.Name, err))
		}
	}
	return failures
}
