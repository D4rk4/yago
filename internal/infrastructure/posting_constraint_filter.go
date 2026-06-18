package infrastructure

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func matchesConstraint(ctx context.Context, entry yacymodel.RWIEntry, constraint string) bool {
	if constraint == "" {
		return true
	}
	required, err := yacymodel.DecodeBitfield(constraint)
	if err != nil {
		slog.WarnContext(ctx, "rwi constraint discarded", "reason", "decode failed", "error", err)
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
			"reason",
			"appearance flags failed",
			"error",
			err,
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
