package nodestatus

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
)

const msgCountUnavailable = "count unavailable for self seed"

type nodeReport struct {
	id      nodeidentity.Identity
	base    yacymodel.Seed
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

func (r nodeReport) SelfSeed(ctx context.Context) yacymodel.Seed {
	now := r.now()
	seed := r.base
	seed.Uptime = yacymodel.Some(r.id.Uptime(now))
	seed.UTC = yacymodel.Some(yacymodel.SeedUTCOffsetFromTime(now))
	seed.LastSeen = yacymodel.Some(yacymodel.NewSeedLastSeenUTC(now))
	seed.RWICount = yacymodel.Some(countOrZero(ctx, r.sources.RWI.RWICount))
	seed.URLCount = yacymodel.Some(countOrZero(ctx, r.sources.URLs.Count))
	seed.KnownSeedCount = yacymodel.Some(r.sources.Peers.KnownPeerCount(ctx))
	seed.News = yacymodel.Some(r.sources.News.SeedNews(ctx))
	seed.NoticedURLCount = yacymodel.Some(0)
	seed.OfferedURLCount = yacymodel.Some(0)
	seed.ConnectsPerHour = yacymodel.Some(0)
	seed.IndexingSpeed = yacymodel.Some(0)
	seed.RequestSpeed = yacymodel.Some(0)
	seed.UplinkSpeed = yacymodel.Some(0)

	transfers := r.sources.Transfers.TransferTotals(ctx)
	seed.SentWordCount = yacymodel.Some(transfers.SentWords)
	seed.ReceivedWordCount = yacymodel.Some(transfers.ReceivedWords)
	seed.SentURLCount = yacymodel.Some(transfers.SentURLs)
	seed.ReceivedURLCount = yacymodel.Some(transfers.ReceivedURLs)

	return seed
}

func baseSeed(id nodeidentity.Identity) yacymodel.Seed {
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
	if !id.BirthDate.IsZero() {
		seed.BirthDate = yacymodel.Some(yacymodel.NewSeedBirthDateUTC(id.BirthDate))
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
