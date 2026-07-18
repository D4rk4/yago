package documentsearch

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
)

const msgAppearanceDiscarded = "rwi search posting discarded"

type termAppearance struct {
	documentIdentifier   yagomodel.Hash
	documentLocation     yagomodel.URLHash
	posting              yagomodel.RWIPosting
	occurrences          uint64
	textPosition         uint64
	language             string
	contentKind          byte
	contentKindKnown     bool
	appearanceFlags      yagomodel.Bitfield
	appearanceFlagsError error
}

func translateAppearance(
	ctx context.Context,
	posting yagomodel.RWIPosting,
) (termAppearance, bool) {
	location, err := posting.URLHash()
	if err != nil {
		slog.WarnContext(ctx, msgAppearanceDiscarded,
			slog.String("reason", "invalid url hash"),
			slog.Any("error", err),
		)

		return termAppearance{}, false
	}

	occurrences, ok := cardinal(ctx, posting, yagomodel.ColHitCount)
	if !ok {
		return termAppearance{}, false
	}
	textPosition, ok := cardinal(ctx, posting, yagomodel.ColTextPosition)
	if !ok {
		return termAppearance{}, false
	}

	appearance := termAppearance{
		documentIdentifier: location.Hash(),
		documentLocation:   location,
		posting:            posting,
		occurrences:        occurrences,
		textPosition:       textPosition,
		language:           posting.Properties[yagomodel.ColLanguage],
	}
	appearance.contentKind, appearance.contentKindKnown = posting.DocType()
	appearance.appearanceFlags, appearance.appearanceFlagsError = posting.AppearanceFlags()

	return appearance, true
}

func cardinal(ctx context.Context, posting yagomodel.RWIPosting, field string) (uint64, bool) {
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
