package urlmeta

import (
	"context"
	"log/slog"
)

const (
	urlDiscardInvalidHash = "invalid url hash"
	urlDiscardStoreFailed = "store failed"
)

type urlRowDiscard struct {
	reason string
	err    error
}

func logURLRowDiscards(ctx context.Context, discards []urlRowDiscard) {
	for _, discard := range discards {
		slog.WarnContext(ctx, urlRowDiscarded,
			slog.String("reason", discard.reason),
			slog.Any("error", discard.err),
		)
	}
}
