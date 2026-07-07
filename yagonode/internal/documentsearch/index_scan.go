package documentsearch

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
)

func (s searcher) scanTerm(
	ctx context.Context,
	term yagomodel.Hash,
	appearanceCriteria termAppearanceCriteria,
) ([]termAppearance, int, error) {
	var (
		kept  []termAppearance
		total int
	)
	err := s.index.ScanWord(ctx, term, func(posting yagomodel.RWIPosting) (bool, error) {
		appearance, ok := translateAppearance(ctx, posting)
		if !ok || !appearanceCriteria.matches(ctx, appearance) {
			return true, nil
		}
		total++
		if s.matchesPerTerm > 0 && len(kept) >= s.matchesPerTerm {
			return true, nil
		}
		kept = append(kept, appearance)

		return true, nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("scan word: %w", err)
	}

	return kept, total, nil
}

func (s searcher) excludedDocuments(
	ctx context.Context,
	terms []yagomodel.Hash,
) (map[yagomodel.Hash]struct{}, error) {
	excluded := make(map[yagomodel.Hash]struct{})
	for _, term := range terms {
		err := s.index.ScanWord(ctx, term, func(posting yagomodel.RWIPosting) (bool, error) {
			if location, err := posting.URLHash(); err == nil {
				excluded[location.Hash()] = struct{}{}
			}

			return true, nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan excluded word: %w", err)
		}
	}

	return excluded, nil
}
