package peerannouncement

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

const peerAnsweredEndpointReadFailedMessage = "peer answered endpoint read failed"

type answeredEndpointMemory interface {
	AnsweredEndpoint(ctx context.Context, peer yagomodel.Hash) (peerwire.Endpoint, bool, error)
	ObserveAnsweredResponder(
		ctx context.Context,
		responder yagomodel.Seed,
		endpoint peerwire.Endpoint,
	)
}

func (a *announcer) greetAtAnsweredEndpoint(
	ctx context.Context,
	target yagomodel.Seed,
	self yagomodel.Seed,
) (greetResult, error) {
	result, err := a.greeter.Greet(
		ctx,
		target,
		self,
		announceHelloPeerCount,
		a.rememberedAnsweredEndpoint(ctx, target.Hash),
	)
	if err != nil {
		return greetResult{}, fmt.Errorf("greet peer: %w", err)
	}

	return result, nil
}

func (a *announcer) rememberedAnsweredEndpoint(
	ctx context.Context,
	peer yagomodel.Hash,
) yagomodel.Optional[peerwire.Endpoint] {
	endpoint, found, err := a.roster.AnsweredEndpoint(ctx, peer)
	switch {
	case err != nil:
		slog.WarnContext(
			ctx,
			peerAnsweredEndpointReadFailedMessage,
			slog.String("peer", peer.String()),
			slog.Any("error", err),
		)
	case found:
		return yagomodel.Some(endpoint)
	}

	return yagomodel.None[peerwire.Endpoint]()
}

func (a *announcer) observeGreetedResponder(
	ctx context.Context,
	target yagomodel.Seed,
	result greetResult,
) {
	a.roster.ObserveAnsweredResponder(
		ctx,
		responderAtTargetEndpoint(result.Responder, target, result.ContactedHost),
		result.ContactedEndpoint,
	)
}
