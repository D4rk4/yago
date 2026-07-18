package frontier

import (
	"context"
	"crypto/rand"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func prepareSeedCandidates(
	ctx context.Context,
	requests []yagocrawlcontract.CrawlRequest,
	provenance []byte,
	profile crawladmission.AdmissionProfile,
) []frontierCandidate {
	candidates := make([]frontierCandidate, 0, len(requests))
	for _, request := range requests {
		if request.ProfileHandle != profile.Profile.Handle {
			slog.WarnContext(ctx, msgSeedProfileMismatch,
				slog.String("url", request.URL),
				slog.String("seedProfileHandle", request.ProfileHandle),
				slog.String("orderProfileHandle", profile.Profile.Handle),
			)

			continue
		}
		normalized, ok := weburl.Normalize(request.URL)
		if !ok || len(normalized) > yagocrawlcontract.MaximumCrawlURLBytes {
			slog.WarnContext(ctx, msgSeedURLRejected,
				slog.String("url", request.URL),
				slog.String("profileHandle", request.ProfileHandle),
			)

			continue
		}
		candidates = append(candidates, preparedCandidate(frontierCandidateSource{
			normalized:       normalized,
			depth:            request.Depth,
			profileHandle:    request.ProfileHandle,
			provenance:       provenance,
			sourceModifiedAt: request.LastModified,
		}, profile))
	}

	return candidates
}

func prepareDiscoveredCandidates(
	work crawljob.CrawlJob,
	links crawljob.DiscoveredLinks,
	profile crawladmission.AdmissionProfile,
) []frontierCandidate {
	admitted := profile.AdmitLinks(
		work.URL,
		links.ByPolicy(profile.Profile.FollowNoFollowLinks),
	)
	candidates := make([]frontierCandidate, 0, len(admitted))
	for _, normalized := range admitted {
		candidates = append(candidates, preparedCandidate(frontierCandidateSource{
			normalized:    normalized,
			depth:         work.Depth + 1,
			profileHandle: work.ProfileHandle,
			provenance:    work.Provenance,
		}, profile))
	}

	return candidates
}

type frontierCandidateSource struct {
	normalized       string
	depth            int
	profileHandle    string
	provenance       []byte
	sourceModifiedAt time.Time
}

func preparedCandidate(
	source frontierCandidateSource,
	profile crawladmission.AdmissionProfile,
) frontierCandidate {
	return frontierCandidate{
		normURL:          source.normalized,
		host:             weburl.Host(source.normalized),
		depth:            source.depth,
		profileHandle:    source.profileHandle,
		provenance:       source.provenance,
		sourceModifiedAt: source.sourceModifiedAt,
		indexAllowed:     profile.IndexAllowed(source.normalized),
		observationID:    rand.Text(),
		observedAt:       time.Now().UTC(),
	}
}
