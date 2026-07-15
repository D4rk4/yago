package httpguard

import (
	"context"
	"errors"
	"log/slog"
	"syscall"
)

const (
	msgWireResponseWriteFailed         = "wire response write failed"
	wireResponseClientDisconnectReason = "client_disconnect"
)

func reportWireResponseWriteFailure(ctx context.Context, err error) {
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		slog.DebugContext(ctx, msgWireResponseWriteFailed,
			slog.String("reason", wireResponseClientDisconnectReason))

		return
	}
	slog.WarnContext(ctx, msgWireResponseWriteFailed, slog.Any("error", err))
}
