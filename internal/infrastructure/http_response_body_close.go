package infrastructure

import (
	"context"
	"io"
	"log/slog"
)

func closeResponseBody(ctx context.Context, body io.Closer, message string) {
	if err := body.Close(); err != nil {
		slog.WarnContext(ctx, message, "error", err)
	}
}
