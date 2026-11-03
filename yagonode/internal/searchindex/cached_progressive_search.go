package searchindex

import (
	"context"
	"fmt"
)

func (c *CachedSearchIndex) SearchProgressively(
	ctx context.Context,
	req SearchRequest,
	notify func(SearchProgress),
) (SearchResultSet, error) {
	progressive, ok := c.inner.(ProgressiveSearchIndex)
	if !ok {
		return SearchResultSet{}, fmt.Errorf("progressive search unavailable")
	}

	result, err := progressive.SearchProgressively(ctx, req, notify)
	if err != nil {
		return result, fmt.Errorf("cached progressive search: %w", err)
	}

	return result, nil
}
