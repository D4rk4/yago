package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type httpShutdownResult struct {
	position int
	err      error
}

func shutdown(servers []namedServer) error {
	return shutdownWithin(
		servers,
		shutdownTimeout-shutdownForcedWait,
		shutdownForcedWait,
	)
}

func shutdownWithin(
	servers []namedServer,
	gracefulWait time.Duration,
	forcedWait time.Duration,
) error {
	slog.InfoContext(context.Background(), "shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), gracefulWait)
	defer cancel()
	results := make(chan httpShutdownResult, len(servers))
	for position, server := range servers {
		go func() {
			results <- httpShutdownResult{
				position: position,
				err:      shutdownHTTPRequestsWithin(ctx, server, forcedWait),
			}
		}()
	}
	failures := make([]error, len(servers))
	for range servers {
		result := <-results
		failures[result.position] = result.err
	}

	return errors.Join(failures...)
}

func shutdownHTTPRequestsWithin(
	ctx context.Context,
	server namedServer,
	forcedWait time.Duration,
) error {
	requests, _ := server.server.Handler.(*httpRequestLifecycle)
	if requests != nil {
		requests.stopAccepting()
	}
	shutdownError := shutdownHTTPServer(server.server, ctx)
	var closeError error
	if shutdownError != nil {
		closeError = closeHTTPServer(server.server)
	}
	drainError := waitForHTTPRequests(requests, forcedWait)

	return resolveHTTPShutdown(server.name, shutdownError, closeError, drainError)
}

func wrapHTTPShutdownError(service string, operation string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s %s: %w", operation, service, err)
}
