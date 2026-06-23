package nodestatus

import (
	"context"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const msgCountUnavailable = "count unavailable for self seed"

type nodeReport struct {
	id   Identity
	base yacymodel.Seed
	now  func() time.Time
	rwi  RWICounter
	urls URLCounter
}

func newReport(id Identity, now func() time.Time, rwi RWICounter, urls URLCounter) nodeReport {
	return nodeReport{
		id:   id,
		base: baseSeed(id),
		now:  now,
		rwi:  rwi,
		urls: urls,
	}
}

func (r nodeReport) Version(context.Context) string {
	return r.id.Version
}

func (r nodeReport) Uptime(context.Context) int {
	return r.id.Uptime(r.now())
}

func (r nodeReport) SelfSeed(ctx context.Context) yacymodel.Seed {
	now := r.now()
	seed := r.base
	seed.Uptime = yacymodel.Some(r.id.Uptime(now))
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
