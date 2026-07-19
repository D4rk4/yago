package remotecrawl

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
)

const remoteCrawlDecisionMessage = "remote crawl decision"

type Observation struct {
	Action  string
	Outcome string
	Peer    yagomodel.Hash
	URLHash yagomodel.Hash
	Count   int
}

type Observer interface {
	ObserveRemoteCrawl(Observation)
}

func (b *Broker) observe(ctx context.Context, observation Observation, warning bool) {
	arguments := []any{
		slog.String("action", observation.Action),
		slog.String("outcome", observation.Outcome),
		slog.Int("count", observation.Count),
	}
	if observation.Peer != "" {
		arguments = append(arguments, slog.String("peer", observation.Peer.String()))
	}
	if observation.URLHash != "" {
		arguments = append(arguments, slog.String("urlHash", observation.URLHash.String()))
	}
	if warning {
		slog.WarnContext(ctx, remoteCrawlDecisionMessage, arguments...)
	} else {
		slog.DebugContext(ctx, remoteCrawlDecisionMessage, arguments...)
	}
	for _, observer := range b.observers {
		if observer != nil {
			observer.ObserveRemoteCrawl(observation)
		}
	}
}
