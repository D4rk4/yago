package infrastructure

import (
	"context"
	"log/slog"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type postingSearchMatcher struct {
	query    ports.PostingSearchQuery
	allowed  map[yacymodel.Hash]struct{}
	excluded map[yacymodel.Hash]struct{}
}

func newPostingSearchMatcher(
	ctx context.Context,
	bucket *bolt.Bucket,
	query ports.PostingSearchQuery,
) (postingSearchMatcher, error) {
	excluded, err := excludedURLHashes(ctx, bucket, query.ExcludeHashes)
	if err != nil {
		return postingSearchMatcher{}, err
	}
	return postingSearchMatcher{
		query:    query,
		allowed:  hashSet(query.URLHashes),
		excluded: excluded,
	}, nil
}

func (m postingSearchMatcher) matches(
	ctx context.Context,
	entry yacymodel.RWIPosting,
) bool {
	query := m.query
	if query.Language != "" && entry.Properties[yacymodel.ColLanguage] != query.Language {
		return false
	}
	distance, err := entry.Cardinal(yacymodel.ColWordDistance)
	if err != nil {
		slog.WarnContext(
			ctx,
			"rwi filter field discarded",
			slog.String("field", yacymodel.ColWordDistance),
			slog.Any("error", err),
		)
		distance = 0
	}
	if query.MaxDistance > 0 && distance > uint64(query.MaxDistance) {
		return false
	}
	urlHash, err := entry.URLHash()
	if err != nil {
		slog.WarnContext(
			ctx,
			"rwi search posting discarded",
			slog.String("reason", "invalid url hash"),
			slog.Any("error", err),
		)
		return false
	}
	if len(m.allowed) != 0 {
		if _, ok := m.allowed[urlHash]; !ok {
			return false
		}
	}
	if _, ok := m.excluded[urlHash]; ok {
		return false
	}
	if !matchesSiteHash(urlHash, query.SiteHash) {
		return false
	}
	if !matchesContentDomain(ctx, entry, query.ContentDomain, query.StrictContentDom) {
		return false
	}
	if !matchesConstraint(ctx, entry, query.Constraint) {
		return false
	}
	return true
}

func hashSet(hashes []yacymodel.Hash) map[yacymodel.Hash]struct{} {
	if len(hashes) == 0 {
		return nil
	}
	out := make(map[yacymodel.Hash]struct{}, len(hashes))
	for _, hash := range hashes {
		out[hash] = struct{}{}
	}
	return out
}
