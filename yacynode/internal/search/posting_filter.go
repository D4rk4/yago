package search

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
)

type postingFilter struct {
	query    postingQuery
	allowed  map[yacymodel.Hash]struct{}
	excluded map[yacymodel.Hash]struct{}
}

func newPostingFilter(
	ctx context.Context,
	index rwi.PostingScanner,
	query postingQuery,
) (postingFilter, error) {
	excluded, err := excludedURLHashes(ctx, index, query.excludeHashes)
	if err != nil {
		return postingFilter{}, err
	}

	return postingFilter{
		query:    query,
		allowed:  hashSet(query.urlHashes),
		excluded: excluded,
	}, nil
}

func (m postingFilter) matches(ctx context.Context, entry yacymodel.RWIPosting) bool {
	query := m.query
	if query.language != "" && entry.Properties[yacymodel.ColLanguage] != query.language {
		return false
	}
	distance, err := entry.Cardinal(yacymodel.ColWordDistance)
	if err != nil {
		slog.WarnContext(ctx, "rwi filter field discarded",
			slog.String("field", yacymodel.ColWordDistance),
			slog.Any("error", err),
		)
		distance = 0
	}
	if query.maxDistance > 0 && distance > uint64(query.maxDistance) {
		return false
	}
	urlHash, err := entry.URLHash()
	if err != nil {
		slog.WarnContext(ctx, "rwi search posting discarded",
			slog.String("reason", "invalid url hash"),
			slog.Any("error", err),
		)

		return false
	}
	if len(m.allowed) != 0 {
		if _, ok := m.allowed[urlHash.Hash()]; !ok {
			return false
		}
	}
	if _, ok := m.excluded[urlHash.Hash()]; ok {
		return false
	}
	if !matchesSiteHash(urlHash, query.siteHash) {
		return false
	}
	if !matchesContentDomain(ctx, entry, query.contentDomain, query.strictContentDom) {
		return false
	}

	return matchesConstraint(ctx, entry, query.constraint)
}

func matchesSiteHash(urlHash yacymodel.URLHash, siteHash string) bool {
	if siteHash == "" {
		return true
	}
	hostHash, err := urlHash.HostHash()

	return err == nil && hostHash == siteHash
}

func matchesContentDomain(
	ctx context.Context,
	entry yacymodel.RWIPosting,
	domain string,
	strict bool,
) bool {
	switch domain {
	case "image":
		if strict {
			return hasDocType(entry, yacymodel.DocTypeImage)
		}

		return hasAppearanceFlag(ctx, entry, yacymodel.RWIFlagHasImage)
	case "audio":
		if strict {
			return hasDocType(entry, yacymodel.DocTypeAudio)
		}

		return hasAppearanceFlag(ctx, entry, yacymodel.RWIFlagHasAudio)
	case "video":
		if strict {
			return hasDocType(entry, yacymodel.DocTypeMovie)
		}

		return hasAppearanceFlag(ctx, entry, yacymodel.RWIFlagHasVideo)
	case "app":
		return hasAppearanceFlag(ctx, entry, yacymodel.RWIFlagHasApp)
	default:
		return true
	}
}

func hasDocType(entry yacymodel.RWIPosting, want byte) bool {
	got, ok := entry.DocType()

	return ok && got == want
}

func hasAppearanceFlag(ctx context.Context, entry yacymodel.RWIPosting, bit int) bool {
	flags, err := entry.AppearanceFlags()
	if err != nil {
		slog.WarnContext(ctx, "rwi content domain candidate discarded",
			slog.String("reason", "appearance flags failed"),
			slog.Any("error", err),
		)

		return false
	}

	return flags.Get(bit)
}

func matchesConstraint(ctx context.Context, entry yacymodel.RWIPosting, constraint string) bool {
	if constraint == "" {
		return true
	}
	required, err := yacymodel.DecodeBitfield(constraint)
	if err != nil {
		slog.WarnContext(ctx, "rwi constraint discarded",
			slog.String("reason", "decode failed"),
			slog.Any("error", err),
		)

		return true
	}
	if required.AllSet(yacymodel.RWIFlagBitCount) {
		return true
	}
	flags, err := entry.AppearanceFlags()
	if err != nil {
		slog.WarnContext(ctx, "rwi constraint candidate discarded",
			slog.String("reason", "appearance flags failed"),
			slog.Any("error", err),
		)

		return false
	}
	for bit := range yacymodel.RWIFlagBitCount {
		if required.Get(bit) && flags.Get(bit) {
			return true
		}
	}

	return false
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
