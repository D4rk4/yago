package infrastructure

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

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
		slog.WarnContext(
			ctx,
			"rwi content domain candidate discarded",
			slog.String("reason", "appearance flags failed"),
			slog.Any("error", err),
		)
		return false
	}
	return flags.Get(bit)
}
