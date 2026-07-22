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

func (r nodeReport) UptimeSeconds(context.Context) int {
	return r.id.UptimeSeconds(r.now())
}

func (r nodeReport) PublishedPeerType(ctx context.Context) yagomodel.PeerType {
	if r.sources.PeerClassification == nil {
		return yagomodel.PeerVirgin
	}
	peerType := r.sources.PeerClassification.PublishedPeerType(ctx)
	switch peerType {
	case yagomodel.PeerVirgin,
		yagomodel.PeerJunior,
		yagomodel.PeerSenior,
		yagomodel.PeerPrincipal:
		return peerType
	default:
		return yagomodel.PeerVirgin
	}
}

func (r nodeReport) SelfSeed(ctx context.Context) yagomodel.Seed {
	now := r.now()
	seed := r.base
	seed.PeerType = yagomodel.Some(r.PublishedPeerType(ctx))
	seed.Uptime = yagomodel.Some(r.id.Uptime(now))
	seed.UTC = yagomodel.Some(yagomodel.SeedUTCOffsetFromTime(now))
	seed.LastSeen = yagomodel.Some(yagomodel.NewSeedLastSeenUTC(now))
	seed.RWICount = countSeedStatistic(ctx, r.sources.RWI.RWICount)
	seed.URLCount = countSeedStatistic(ctx, r.sources.URLs.Count)
	seed.KnownSeedCount = yagomodel.Some(max(0, r.sources.Peers.ReachablePeerCount(ctx)))
	seed.News = yagomodel.Some(r.sources.News.SeedNews(ctx))
	if r.sources.Queues != nil {
		queues := r.sources.Queues.SeedQueueStatistics(ctx)
		if queues.NoticedKnown {
			seed.NoticedURLCount = yagomodel.Some(max(0, queues.Noticed))
		}
		if queues.OfferedKnown {
			seed.OfferedURLCount = yagomodel.Some(max(0, queues.Offered))
		}
	}

	transfers := r.sources.Transfers.TransferTotals(ctx)
	if transfers.Known {
		seed.SentWordCount = yagomodel.Some(transfers.SentWords)
		seed.ReceivedWordCount = yagomodel.Some(transfers.ReceivedWords)
		seed.SentURLCount = yagomodel.Some(transfers.SentURLs)
		seed.ReceivedURLCount = yagomodel.Some(transfers.ReceivedURLs)
	}

	return seed
}

func baseSeed(id nodeidentity.Identity) yagomodel.Seed {
	seed := yagomodel.Seed{
		Hash:     id.Hash,
		Name:     yagomodel.Some(id.Name),
		Port:     yagomodel.Some(yagomodel.Port(id.Port)),
		Flags:    yagomodel.Some(id.Flags),
		PeerType: yagomodel.Some(yagomodel.PeerVirgin),
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

func countSeedStatistic(
	ctx context.Context,
	fn func(context.Context) (int, error),
) yagomodel.Optional[int] {
	n, err := fn(ctx)
	if err != nil {
		slog.WarnContext(ctx, msgCountUnavailable, slog.Any("error", err))

		return yagomodel.None[int]()
	}

	return yagomodel.Some(max(0, n))
}
