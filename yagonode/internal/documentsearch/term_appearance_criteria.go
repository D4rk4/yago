package documentsearch

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	msgAppearanceFlagsDiscarded = "rwi appearance flags discarded"
	msgSiteHostUndetermined     = "rwi search posting site host undetermined"
)

type termAppearanceCriteria struct {
	language           string
	requiredDocuments  map[yagomodel.Hash]struct{}
	excludedDocuments  map[yagomodel.Hash]struct{}
	siteHashes         []string
	contentKind        contentKind
	strictContentKind  bool
	requiredProperties yagomodel.Bitfield
}

func (s searcher) appearanceCriteria(
	ctx context.Context,
	criteria searchCriteria,
	excludedTerms []yagomodel.Hash,
) (termAppearanceCriteria, error) {
	excluded, err := s.excludedDocuments(ctx, excludedTerms)
	if err != nil {
		return termAppearanceCriteria{}, err
	}

	return termAppearanceCriteria{
		language:           criteria.language,
		requiredDocuments:  documentSet(criteria.requiredDocuments),
		excludedDocuments:  excluded,
		siteHashes:         criteria.siteHashes,
		contentKind:        criteria.contentKind,
		strictContentKind:  criteria.strictContentKind,
		requiredProperties: criteria.requiredProperties,
	}, nil
}

func (c termAppearanceCriteria) matches(ctx context.Context, appearance termAppearance) bool {
	if c.language != "" && appearance.language != c.language {
		return false
	}
	if len(c.requiredDocuments) != 0 {
		if _, ok := c.requiredDocuments[appearance.documentIdentifier]; !ok {
			return false
		}
	}
	if _, ok := c.excludedDocuments[appearance.documentIdentifier]; ok {
		return false
	}
	if !matchesSiteHost(ctx, appearance.documentLocation, c.siteHashes) {
		return false
	}
	if !matchesContentKind(ctx, appearance, c.contentKind, c.strictContentKind) {
		return false
	}

	return matchesRequiredProperties(ctx, appearance, c.requiredProperties)
}

func matchesSiteHost(ctx context.Context, location yagomodel.URLHash, siteHashes []string) bool {
	if len(siteHashes) == 0 {
		return true
	}
	hostHash, err := location.HostHash()
	if err != nil {
		slog.WarnContext(ctx, msgSiteHostUndetermined, slog.Any("error", err))

		return false
	}

	for _, siteHash := range siteHashes {
		if hostHash == siteHash {
			return true
		}
	}

	return false
}

func matchesContentKind(
	ctx context.Context,
	appearance termAppearance,
	kind contentKind,
	strict bool,
) bool {
	switch kind {
	case imageContent:
		if strict {
			return appearance.hasContentKind(yagomodel.DocTypeImage)
		}

		return appearance.hasAppearanceFlag(ctx, yagomodel.RWIFlagHasImage)
	case audioContent:
		if strict {
			return appearance.hasContentKind(yagomodel.DocTypeAudio)
		}

		return appearance.hasAppearanceFlag(ctx, yagomodel.RWIFlagHasAudio)
	case videoContent:
		if strict {
			return appearance.hasContentKind(yagomodel.DocTypeMovie)
		}

		return appearance.hasAppearanceFlag(ctx, yagomodel.RWIFlagHasVideo)
	case applicationContent:
		return appearance.hasAppearanceFlag(ctx, yagomodel.RWIFlagHasApp)
	default:
		return true
	}
}

func matchesRequiredProperties(
	ctx context.Context,
	appearance termAppearance,
	required yagomodel.Bitfield,
) bool {
	if required == nil {
		return true
	}
	flags, ok := appearance.flags(ctx)
	if !ok {
		return false
	}
	for bit := range yagomodel.RWIFlagBitCount {
		if required.Get(bit) && flags.Get(bit) {
			return true
		}
	}

	return false
}

func (a termAppearance) hasContentKind(want byte) bool {
	return a.contentKindKnown && a.contentKind == want
}

func (a termAppearance) hasAppearanceFlag(ctx context.Context, bit int) bool {
	flags, ok := a.flags(ctx)

	return ok && flags.Get(bit)
}

func (a termAppearance) flags(ctx context.Context) (yagomodel.Bitfield, bool) {
	if a.appearanceFlagsError != nil {
		slog.WarnContext(ctx, msgAppearanceFlagsDiscarded,
			slog.Any("error", a.appearanceFlagsError),
		)

		return nil, false
	}

	return a.appearanceFlags, true
}

func documentSet(identifiers []yagomodel.Hash) map[yagomodel.Hash]struct{} {
	if len(identifiers) == 0 {
		return nil
	}
	set := make(map[yagomodel.Hash]struct{}, len(identifiers))
	for _, identifier := range identifiers {
		set[identifier] = struct{}{}
	}

	return set
}
