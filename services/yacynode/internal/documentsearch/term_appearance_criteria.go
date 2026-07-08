package documentsearch

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const (
	msgAppearanceFlagsDiscarded = "rwi appearance flags discarded"
	msgSiteHostUndetermined     = "rwi search posting site host undetermined"
)

type termAppearanceCriteria struct {
	language           string
	requiredDocuments  map[yacymodel.Hash]struct{}
	excludedDocuments  map[yacymodel.Hash]struct{}
	siteHash           string
	contentKind        contentKind
	strictContentKind  bool
	requiredProperties yacymodel.Bitfield
}

func (s searcher) appearanceCriteria(
	ctx context.Context,
	criteria searchCriteria,
	excludedTerms []yacymodel.Hash,
) (termAppearanceCriteria, error) {
	excluded, err := s.excludedDocuments(ctx, excludedTerms)
	if err != nil {
		return termAppearanceCriteria{}, err
	}

	return termAppearanceCriteria{
		language:           criteria.language,
		requiredDocuments:  documentSet(criteria.requiredDocuments),
		excludedDocuments:  excluded,
		siteHash:           criteria.siteHash,
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
	if !matchesSiteHost(ctx, appearance.documentLocation, c.siteHash) {
		return false
	}
	if !matchesContentKind(ctx, appearance, c.contentKind, c.strictContentKind) {
		return false
	}

	return matchesRequiredProperties(ctx, appearance, c.requiredProperties)
}

func matchesSiteHost(ctx context.Context, location yacymodel.URLHash, siteHash string) bool {
	if siteHash == "" {
		return true
	}
	hostHash, err := location.HostHash()
	if err != nil {
		slog.WarnContext(ctx, msgSiteHostUndetermined, slog.Any("error", err))

		return false
	}

	return hostHash == siteHash
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
			return appearance.hasContentKind(yacymodel.DocTypeImage)
		}

		return appearance.hasAppearanceFlag(ctx, yacymodel.RWIFlagHasImage)
	case audioContent:
		if strict {
			return appearance.hasContentKind(yacymodel.DocTypeAudio)
		}

		return appearance.hasAppearanceFlag(ctx, yacymodel.RWIFlagHasAudio)
	case videoContent:
		if strict {
			return appearance.hasContentKind(yacymodel.DocTypeMovie)
		}

		return appearance.hasAppearanceFlag(ctx, yacymodel.RWIFlagHasVideo)
	case applicationContent:
		return appearance.hasAppearanceFlag(ctx, yacymodel.RWIFlagHasApp)
	default:
		return true
	}
}

func matchesRequiredProperties(
	ctx context.Context,
	appearance termAppearance,
	required yacymodel.Bitfield,
) bool {
	if required == nil {
		return true
	}
	flags, ok := appearance.flags(ctx)
	if !ok {
		return false
	}
	for bit := range yacymodel.RWIFlagBitCount {
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

func (a termAppearance) flags(ctx context.Context) (yacymodel.Bitfield, bool) {
	if a.appearanceFlagsError != nil {
		slog.WarnContext(ctx, msgAppearanceFlagsDiscarded,
			slog.Any("error", a.appearanceFlagsError),
		)

		return nil, false
	}

	return a.appearanceFlags, true
}

func documentSet(identifiers []yacymodel.Hash) map[yacymodel.Hash]struct{} {
	if len(identifiers) == 0 {
		return nil
	}
	set := make(map[yacymodel.Hash]struct{}, len(identifiers))
	for _, identifier := range identifiers {
		set[identifier] = struct{}{}
	}

	return set
}
