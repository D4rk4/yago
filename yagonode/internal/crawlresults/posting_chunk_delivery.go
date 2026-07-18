package crawlresults

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
)

func receivePostingChunks(
	ctx context.Context,
	receiver rwi.PostingReceiver,
	postings []yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	if len(postings) == 0 {
		receipt, err := receiver.Receive(ctx, nil)
		if err != nil {
			return rwi.Receipt{}, fmt.Errorf("receive empty posting chunk: %w", err)
		}

		return receipt, nil
	}

	var combined rwi.Receipt
	for first := 0; first < len(postings); first += yagocrawlcontract.MaximumIngestPostings {
		if err := ctx.Err(); err != nil {
			return rwi.Receipt{}, fmt.Errorf("receive posting chunks: %w", err)
		}
		last := min(first+yagocrawlcontract.MaximumIngestPostings, len(postings))
		receipt, err := receiver.Receive(ctx, postings[first:last])
		if err != nil {
			return rwi.Receipt{}, fmt.Errorf("receive posting chunk: %w", err)
		}
		combined.UnknownURL = append(combined.UnknownURL, receipt.UnknownURL...)
		combined.Pause = max(combined.Pause, receipt.Pause)
		if receipt.Busy {
			combined.Busy = true

			return combined, nil
		}
	}

	return combined, nil
}
