package searchindex

import (
	"context"
	"fmt"
)

func (c *CachedSearchIndex) DocumentPassage(
	ctx context.Context,
	req DocumentPassageRequest,
) (DocumentPassage, bool, error) {
	source, ok := c.inner.(DocumentPassageSource)
	if !ok {
		return DocumentPassage{}, false, fmt.Errorf("document passage unavailable")
	}
	passage, found, err := source.DocumentPassage(ctx, req)
	if err != nil {
		return DocumentPassage{}, false, fmt.Errorf("cached document passage: %w", err)
	}

	return passage, found, nil
}
