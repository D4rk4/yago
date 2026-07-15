package yagonode

import (
	"context"
	"errors"
	"log/slog"
)

const (
	peerBlockFanoutReadFailedMessage  = "read peer blocklist for fan-out failed"
	peerBlockFanoutReadSkippedMessage = "read peer blocklist for fan-out skipped"
	peerBlockFanoutCanceledReason     = "request_canceled"
	peerBlockFanoutDeadlineReason     = "request_deadline"
)

func peerBlockFanoutRequestEnded(ctx context.Context) bool {
	requestErr := ctx.Err()
	if requestErr == nil {
		return false
	}
	reason := peerBlockFanoutDeadlineReason
	if errors.Is(requestErr, context.Canceled) {
		reason = peerBlockFanoutCanceledReason
	}
	slog.DebugContext(ctx, peerBlockFanoutReadSkippedMessage, slog.String("reason", reason))

	return true
}
