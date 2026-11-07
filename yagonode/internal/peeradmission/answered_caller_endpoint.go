package peeradmission

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

const callerAnsweredEndpointReadFailedMessage = "caller answered endpoint read failed"

type AnsweredEndpoints interface {
	AnsweredEndpoint(ctx context.Context, peer yagomodel.Hash) (peerwire.Endpoint, bool, error)
	ObserveAnsweredCaller(
		ctx context.Context,
		caller yagomodel.Seed,
		classification yagomodel.PeerType,
		endpoint peerwire.Endpoint,
	)
}

func (e helloEndpoint) answeredCallerEndpoint(
	ctx context.Context,
	caller yagomodel.Hash,
) yagomodel.Optional[peerwire.Endpoint] {
	endpoint, found, err := e.reachability.AnsweredEndpoint(ctx, caller)
	if err != nil {
		slog.WarnContext(
			ctx,
			callerAnsweredEndpointReadFailedMessage,
			slog.String("peer", caller.String()),
			slog.Any("error", err),
		)

		return yagomodel.None[peerwire.Endpoint]()
	}
	if !found {
		return yagomodel.None[peerwire.Endpoint]()
	}

	return yagomodel.Some(endpoint)
}

func (e helloEndpoint) observeClassifiedCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
	answered yagomodel.Optional[peerwire.Endpoint],
) {
	if endpoint, ok := answered.Get(); ok {
		e.reachability.ObserveAnsweredCaller(ctx, caller, classification, endpoint)

		return
	}
	e.reachability.ObserveCaller(ctx, caller, classification)
}
