package eviction

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/urlreferences"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type urlProjectionDeletion struct {
	postings   rwi.PostingPurger
	references urlreferences.ReferenceQuery
	metadata   urlmeta.URLEvictor
}

func (d urlProjectionDeletion) delete(
	ctx context.Context,
	tx *vault.Txn,
	urls []yagomodel.Hash,
) (Result, urlmeta.PurgeResult, error) {
	result := Result{}
	for _, url := range urls {
		words, err := d.references.WordsReferencing(tx, url)
		if err != nil {
			return Result{}, urlmeta.PurgeResult{}, fmt.Errorf("words referencing url: %w", err)
		}
		for _, word := range words {
			deleted, err := d.postings.PurgePosting(tx, word, url)
			if err != nil {
				return Result{}, urlmeta.PurgeResult{}, fmt.Errorf("purge posting: %w", err)
			}
			if deleted {
				result.PostingsDeleted++
			}
		}
	}
	metadata, err := d.metadata.Purge(ctx, tx, urls)
	if err != nil {
		return Result{}, urlmeta.PurgeResult{}, fmt.Errorf("purge urls: %w", err)
	}
	result.URLsDeleted = metadata.URLsDeleted

	return result, metadata, nil
}
