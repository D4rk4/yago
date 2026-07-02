package documentsearch

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func (s searcher) scanTerm(
	ctx context.Context,
	term yacymodel.Hash,
	appearanceCriteria termAppearanceCriteria,
) ([]termAppearance, int, error) {
	// The per-term cap keeps the most frequent appearances rather than the first
	// scanned; an exact join under a memory bound would instead pivot on the rarest term.
	kept := mostFrequentAppearances{limit: s.matchesPerTerm}
	var total int
	err := s.index.ScanWord(ctx, term, func(posting yacymodel.RWIPosting) (bool, error) {
		appearance, ok := translateAppearance(ctx, posting)
		if !ok || !appearanceCriteria.matches(ctx, appearance) {
			return true, nil
		}
		total++
		kept.consider(appearance)

		return true, nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("scan word: %w", err)
	}

	return kept.collected(), total, nil
}

func (s searcher) excludedDocuments(
	ctx context.Context,
	terms []yacymodel.Hash,
) (map[yacymodel.Hash]struct{}, error) {
	excluded := make(map[yacymodel.Hash]struct{})
	for _, term := range terms {
		err := s.index.ScanWord(ctx, term, func(posting yacymodel.RWIPosting) (bool, error) {
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
