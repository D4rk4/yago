package infrastructure

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func matchesConstraint(ctx context.Context, entry yacymodel.RWIPosting, constraint string) bool {
	if constraint == "" {
		return true
	}
	required, err := yacymodel.DecodeBitfield(constraint)
	if err != nil {
		slog.WarnContext(
			ctx,
			"rwi constraint discarded",
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
		slog.WarnContext(
			ctx,
			"rwi constraint candidate discarded",
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
