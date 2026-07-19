package searchremote

import (
	"context"

	"github.com/D4rk4/yago/yagoproto"
)

func (s searcher) observeReceivedResponse(
	ctx context.Context,
	response yagoproto.SearchResponse,
) {
	if s.observeReceivedResources == nil || len(response.Resources) == 0 {
		return
	}

	s.observeReceivedResources(ctx, len(response.Resources))
}
