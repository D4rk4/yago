package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
)

type dhtOutboundRWIWords struct {
	postings rwi.OutboundPostingStore
}

func (w dhtOutboundRWIWords) SelectOutboundWords(
	ctx context.Context,
	maxWords int,
	maxPostings int,
) ([]yagomodel.WordPostings, error) {
	selection, err := w.postings.SelectOutbound(
		ctx,
		rwi.OutboundSelectionConfig{MaxWords: maxWords, MaxPostings: maxPostings},
	)
	if err != nil {
		return nil, fmt.Errorf("select outbound rwi words: %w", err)
	}

	return selection.Words, nil
}

func (w dhtOutboundRWIWords) RestoreOutboundWords(
	ctx context.Context,
	words []yagomodel.WordPostings,
) (int, error) {
	restored, err := w.postings.RestoreOutbound(ctx, words)
	if err != nil {
		return 0, fmt.Errorf("restore outbound rwi words: %w", err)
	}

	return restored, nil
}

func (w dhtOutboundRWIWords) ConfirmTransferred(
	ctx context.Context,
	postings []yagomodel.RWIPosting,
) (int, error) {
	confirmed, err := w.postings.ConfirmOutbound(ctx, postings)
	if err != nil {
		return 0, fmt.Errorf("confirm transferred outbound rwi words: %w", err)
	}

	return confirmed, nil
}
