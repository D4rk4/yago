package nodestatus

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
)

const msgCountUnavailable = "count unavailable for self seed"

type nodeReport struct {
	id      nodeidentity.Identity
	base    yagomodel.Seed
	now     func() time.Time
	sources ReportSources
}

func newReport(
	id nodeidentity.Identity,
	now func() time.Time,
	sources ReportSources,
) nodeReport {
	return nodeReport{
		id:      id,
		base:    baseSeed(id),
		now:     now,
		sources: sources,
	}
}

func (r nodeReport) Version(context.Context) string {
	return r.id.Version
}

func (r nodeReport) Uptime(context.Context) int {
	return r.id.Uptime(r.now())
}

func (r nodeReport) SelfSeed(ctx context.Context) yagomodel.Seed {
	now := r.now()
	seed := r.base
	seed.Uptime = yagomodel.Some(r.id.Uptime(now))
	seed.UTC = yagomodel.Some(yagomodel.SeedUTCOffsetFromTime(now))
	seed.LastSeen = yagomodel.Some(yagomodel.NewSeedLastSeenUTC(now))
	seed.RWICount = yagomodel.Some(countOrZero(ctx, r.sources.RWI.RWICount))
	seed.URLCount = yagomodel.Some(countOrZero(ctx, r.sources.URLs.Count))
	seed.KnownSeedCount = yagomodel.Some(r.sources.Peers.KnownPeerCount(ctx))
	seed.News = yagomodel.Some(r.sources.News.SeedNews(ctx))
	seed.NoticedURLCount = yagomodel.Some(0)
	seed.OfferedURLCount = yagomodel.Some(0)
	seed.ConnectsPerHour = yagomodel.Some(0)
	seed.IndexingSpeed = yagomodel.Some(0)
	seed.RequestSpeed = yagomodel.Some(0)
	seed.UplinkSpeed = yagomodel.Some(0)

	transfers := r.sources.Transfers.TransferTotals(ctx)
	seed.SentWordCount = yagomodel.Some(transfers.SentWords)
	seed.ReceivedWordCount = yagomodel.Some(transfers.ReceivedWords)
	seed.SentURLCount = yagomodel.Some(transfers.SentURLs)
	seed.ReceivedURLCount = yagomodel.Some(transfers.ReceivedURLs)

	return seed
}

func baseSeed(id nodeidentity.Identity) yagomodel.Seed {
	seed := yagomodel.Seed{
		Hash:     id.Hash,
		Name:     yagomodel.Some(id.Name),
		Port:     yagomodel.Some(yagomodel.Port(id.Port)),
		Flags:    yagomodel.Some(id.Flags),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
		Version:  yagomodel.Some(yagomodel.YaCyVersion(id.Version)),
	}
	if host, err := yagomodel.ParseHost(id.Host); err == nil {
		seed.IP = yagomodel.Some(host)
	}
	if !id.BirthDate.IsZero() {
		seed.BirthDate = yagomodel.Some(yagomodel.NewSeedBirthDateUTC(id.BirthDate))
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
