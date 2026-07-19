package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/transfertally"
)

func remoteSearchResourceTally(
	tally *transfertally.Tally,
) func(context.Context, int) {
	if tally == nil {
		return nil
	}

	return func(ctx context.Context, resources int) {
		stable := context.WithoutCancel(ctx)
		tallyTransfer(stable, tally.AddReceivedWords, resources)
		tallyTransfer(stable, tally.AddReceivedURLs, resources)
	}
}
