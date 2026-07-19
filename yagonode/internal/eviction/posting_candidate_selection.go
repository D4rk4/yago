package eviction

import (
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
)

func postingURLCandidates(entries []rwi.StoredPosting) ([]yagomodel.Hash, error) {
	candidates := make([]yagomodel.Hash, 0, len(entries))
	seen := make(map[yagomodel.Hash]struct{}, len(entries))
	for _, entry := range entries {
		url, err := entry.Posting.URLHash()
		if err != nil {
			return nil, fmt.Errorf("posting-only url hash: %w", err)
		}
		if _, found := seen[url.Hash()]; found {
			continue
		}
		seen[url.Hash()] = struct{}{}
		candidates = append(candidates, url.Hash())
	}

	return candidates, nil
}
