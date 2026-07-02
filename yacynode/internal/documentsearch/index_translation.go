package documentsearch

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yacymodel"
)

const msgAppearanceDiscarded = "rwi search posting discarded"

type termAppearance struct {
	documentIdentifier   yacymodel.Hash
	documentLocation     yacymodel.URLHash
	occurrences          uint64
	termSpread           uint64
	language             string
	contentKind          byte
	contentKindKnown     bool
	appearanceFlags      yacymodel.Bitfield
	appearanceFlagsError error
}

func translateAppearance(
	ctx context.Context,
	posting yacymodel.RWIPosting,
) (termAppearance, bool) {
	location, err := posting.URLHash()
	if err != nil {
		slog.WarnContext(ctx, msgAppearanceDiscarded,
			slog.String("reason", "invalid url hash"),
			slog.Any("error", err),
		)

		return termAppearance{}, false
	}

	occurrences, ok := cardinal(ctx, posting, yacymodel.ColHitCount)
	if !ok {
		return termAppearance{}, false
	}
	termSpread, ok := cardinal(ctx, posting, yacymodel.ColWordDistance)
	if !ok {
		return termAppearance{}, false
	}

	appearance := termAppearance{
		documentIdentifier: location.Hash(),
		documentLocation:   location,
		occurrences:        occurrences,
		termSpread:         termSpread,
		language:           posting.Properties[yacymodel.ColLanguage],
	}
	appearance.contentKind, appearance.contentKindKnown = posting.DocType()
	appearance.appearanceFlags, appearance.appearanceFlagsError = posting.AppearanceFlags()

	return appearance, true
}

func cardinal(ctx context.Context, posting yacymodel.RWIPosting, field string) (uint64, bool) {
	value, err := posting.Cardinal(field)
	if err != nil {
		slog.WarnContext(ctx, msgAppearanceDiscarded,
			slog.String("reason", "invalid ranking field"),
			slog.String("field", field),
			slog.Any("error", err),
		)

		return 0, false
	}

	return value, true
}
