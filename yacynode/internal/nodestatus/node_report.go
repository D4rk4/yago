package nodestatus

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const msgCountUnavailable = "count unavailable for self seed"

type nodeReport struct {
	base     yacymodel.Seed
	liveness Liveness
	rwi      RWICounter
	urls     URLCounter
}

func newReport(id Identity, live Liveness, rwi RWICounter, urls URLCounter) nodeReport {
	return nodeReport{
		base:     baseSeed(id),
		liveness: live,
		rwi:      rwi,
		urls:     urls,
	}
}

func (r nodeReport) Version(ctx context.Context) string {
	return r.liveness.Version(ctx)
}

func (r nodeReport) Uptime(ctx context.Context) int {
	return r.liveness.Uptime(ctx)
}

func (r nodeReport) SelfSeed(ctx context.Context) yacymodel.Seed {
	now := r.liveness.now()
	seed := r.base
	seed.Uptime = yacymodel.Some(r.liveness.uptimeMinutes(now))
	seed.UTC = yacymodel.Some(yacymodel.SeedUTCOffsetFromTime(now))
	seed.LastSeen = yacymodel.Some(yacymodel.NewSeedLastSeenUTC(now))
	seed.RWICount = yacymodel.Some(countOrZero(ctx, r.rwi.RWICount))
	seed.URLCount = yacymodel.Some(countOrZero(ctx, r.urls.Count))

	return seed
}

func baseSeed(id Identity) yacymodel.Seed {
	seed := yacymodel.Seed{
		Hash:     id.Hash,
		Name:     yacymodel.Some(id.Name),
		Port:     yacymodel.Some(yacymodel.Port(id.Port)),
		Flags:    yacymodel.Some(id.Flags),
		PeerType: yacymodel.Some(yacymodel.PeerSenior),
		Version:  yacymodel.Some(yacymodel.YaCyVersion(id.Version)),
	}
	if host, err := yacymodel.ParseHost(id.Host); err == nil {
		seed.IP = yacymodel.Some(host)
	}

	return seed
}

func countOrZero(ctx context.Context, fn func(context.Context) (int, error)) int {
	n, err := fn(ctx)
	if err != nil {
		slog.WarnContext(ctx, msgCountUnavailable, slog.Any("error", err))

		return 0
	}

	return n
}
