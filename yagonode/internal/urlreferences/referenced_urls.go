package urlreferences

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (r *urlReferences) ReferencedURLs(
	ctx context.Context,
	urls []yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	referenced := make([]yagomodel.Hash, 0, len(urls))
	seen := make(map[yagomodel.Hash]struct{}, len(urls))
	err := r.vault.View(ctx, func(tx *vault.Txn) error {
		for _, url := range urls {
			if _, ok := seen[url]; ok {
				continue
			}
			seen[url] = struct{}{}

			words, err := r.WordsReferencing(tx, url)
			if err != nil {
				return err
			}
			if len(words) > 0 {
				referenced = append(referenced, url)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("referenced urls: %w", err)
	}

	return referenced, nil
}
