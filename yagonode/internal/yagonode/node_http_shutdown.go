package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type httpShutdownResult struct {
	position int
	err      error
}

func shutdown(servers []namedServer) error {
	slog.InfoContext(context.Background(), "shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	results := make(chan httpShutdownResult, len(servers))
	for position, server := range servers {
		go func() {
			results <- httpShutdownResult{
				position: position,
				err:      shutdownHTTPRequests(ctx, server),
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

func shutdownHTTPRequests(ctx context.Context, server namedServer) error {
	requests, _ := server.server.Handler.(*httpRequestLifecycle)
	if requests != nil {
		requests.stopAccepting()
	}
	shutdownError := shutdownHTTPServer(server.server, ctx)
	var closeError error
	if shutdownError != nil {
		closeError = closeHTTPServer(server.server)
	}
	if requests != nil {
		requests.wait()
	}

	return errors.Join(
		wrapHTTPShutdownError(server.name, "shutdown", shutdownError),
		wrapHTTPShutdownError(server.name, "close", closeError),
	)
}

func wrapHTTPShutdownError(service string, operation string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s %s: %w", operation, service, err)
}
