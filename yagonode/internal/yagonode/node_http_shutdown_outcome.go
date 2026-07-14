package yagonode

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"
)

const msgHTTPShutdownForced = "http server forced closed after shutdown grace elapsed"

func waitForHTTPRequests(requests *httpRequestLifecycle, wait time.Duration) error {
	if requests == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()

	return requests.wait(ctx)
}

func resolveHTTPShutdown(
	service string,
	shutdownError error,
	closeError error,
	drainError error,
) error {
	if forcedHTTPShutdownCompleted(shutdownError, closeError, drainError) {
		slog.WarnContext(
			context.Background(),
			msgHTTPShutdownForced,
			slog.String("service", service),
		)

		return nil
	}

	return errors.Join(
		wrapHTTPShutdownError(service, "shutdown", shutdownError),
		wrapHTTPShutdownError(service, "close", normalizeHTTPServerCloseError(closeError)),
		wrapHTTPShutdownError(service, "drain", drainError),
	)
}

func forcedHTTPShutdownCompleted(
	shutdownError error,
	closeError error,
	drainError error,
) bool {
	return shutdownContextElapsed(shutdownError) &&
		normalizeHTTPServerCloseError(closeError) == nil &&
		drainError == nil
}

func shutdownContextElapsed(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func normalizeHTTPServerCloseError(err error) error {
	if errors.Is(err, net.ErrClosed) {
		return nil
	}

	return err
}
