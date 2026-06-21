package infrastructure

import (
	"context"
	"io"
	"log/slog"
)

const responseBodyCloseFailedMessage = "response body close failed"

func closeResponseBody(ctx context.Context, body io.Closer, operation string) {
	if err := body.Close(); err != nil {
		slog.WarnContext(
			ctx,
			responseBodyCloseFailedMessage,
			slog.String("operation", operation),
			slog.Any("error", err),
		)
	}
}
