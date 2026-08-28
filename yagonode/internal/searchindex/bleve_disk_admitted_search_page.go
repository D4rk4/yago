package searchindex

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2"
)

func (b *BleveDiskIndex) readSearchPage(
	ctx context.Context,
	request *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	release, err := b.admitSearchRead(ctx)
	if err != nil {
		return nil, fmt.Errorf("admit search page: %w", err)
	}
	defer release()

	result, err := b.alias.SearchInContext(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("read search page: %w", err)
	}

	return result, nil
}
