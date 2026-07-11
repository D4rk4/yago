package searchindex

import (
	"context"
	"errors"
	"fmt"

	"github.com/blevesearch/bleve/v2"
)

var errIncompleteBleveSearch = errors.New("bleve search returned incomplete shard results")

func bleveSearchOperationError(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return fmt.Errorf("search context: %w", cause)
	}

	return err
}

func bleveSearchCompletionError(
	ctx context.Context,
	result *bleve.SearchResult,
) error {
	if result == nil {
		return fmt.Errorf("%w: result is absent", errIncompleteBleveSearch)
	}
	if result.Status == nil ||
		(result.Status.Failed == 0 && len(result.Status.Errors) == 0) {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return fmt.Errorf("search context: %w", cause)
	}

	return fmt.Errorf(
		"%w: %d of %d shards failed",
		errIncompleteBleveSearch,
		result.Status.Failed,
		result.Status.Total,
	)
}
