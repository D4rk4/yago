package nodestatus

import (
	"context"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const msgCountUnavailable = "count unavailable for self seed"

type nodeReport struct {
	base    yacymodel.Seed
	version string
	start   time.Time
	now     func() time.Time
	rwi     RWICounter
	urls    URLCounter
}

func newReport(id Identity, rwi RWICounter, urls URLCounter, now func() time.Time) nodeReport {
	return nodeReport{
		base:    baseSeed(id),
		version: id.Version,
		start:   now(),
		now:     now,
		rwi:     rwi,
		urls:    urls,
	}
}

func (r nodeReport) Header(context.Context) yacyproto.ResponseHeader {
	return yacyproto.ResponseHeader{Version: r.version, Uptime: r.uptimeMinutes(r.now())}
}

func (r nodeReport) SelfSeed(ctx context.Context) yacymodel.Seed {
	now := r.now()
	seed := r.base
	seed.Uptime = yacymodel.Some(r.uptimeMinutes(now))
	seed.UTC = yacymodel.Some(yacymodel.SeedUTCOffsetFromTime(now))
	seed.LastSeen = yacymodel.Some(yacymodel.NewSeedLastSeenUTC(now))
	seed.RWICount = yacymodel.Some(countOrZero(ctx, r.rwi.RWICount))
	seed.URLCount = yacymodel.Some(countOrZero(ctx, r.urls.Count))

	return seed
}

func (r nodeReport) uptimeMinutes(now time.Time) int {
	return int(now.Sub(r.start).Minutes())
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
