package search

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type candidate struct {
	hash     yacymodel.Hash
	hits     uint64
	distance uint64
}

func matchWord(ctx context.Context, entries []yacymodel.RWIPosting) map[yacymodel.Hash]candidate {
	matched := make(map[yacymodel.Hash]candidate, len(entries))
	for _, entry := range entries {
		distance, err := entry.Cardinal(yacymodel.ColWordDistance)
		if err != nil {
			slog.WarnContext(ctx, "rwi ranking field discarded",
				slog.String("field", yacymodel.ColWordDistance),
				slog.Any("error", err),
			)
		}
		hits, err := entry.Cardinal(yacymodel.ColHitCount)
		if err != nil {
			slog.WarnContext(ctx, "rwi ranking field discarded",
				slog.String("field", yacymodel.ColHitCount),
				slog.Any("error", err),
			)
		}
		urlHash, err := entry.URLHash()
		if err != nil {
			slog.WarnContext(ctx, "rwi search candidate discarded",
				slog.String("reason", "invalid url hash"),
				slog.Any("error", err),
			)

			continue
		}
		hash := urlHash.Hash()
		if _, seen := matched[hash]; !seen {
			matched[hash] = candidate{hash: hash, hits: hits, distance: distance}
		}
	}

	return matched
}

func intersect(
	joined map[yacymodel.Hash]candidate,
	matched map[yacymodel.Hash]candidate,
) map[yacymodel.Hash]candidate {
	if joined == nil {
		return matched
	}

	for hash, c := range joined {
		next, ok := matched[hash]
		if !ok {
			delete(joined, hash)

			continue
		}
		c.hits += next.hits
		c.distance += next.distance
		joined[hash] = c
	}

	return joined
}

func cloneCandidates(in map[yacymodel.Hash]candidate) map[yacymodel.Hash]candidate {
	out := make(map[yacymodel.Hash]candidate, len(in))
	maps.Copy(out, in)

	return out
}

func rankedHashes(set map[yacymodel.Hash]candidate) []yacymodel.Hash {
	candidates := make([]candidate, 0, len(set))
	for hash, c := range set {
		c.hash = hash
		candidates = append(candidates, c)
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.hits != b.hits {
			return compareDesc(a.hits, b.hits)
		}
		if a.distance != b.distance {
			return compareAsc(a.distance, b.distance)
		}

		return compareAsc(a.hash, b.hash)
	})

	hashes := make([]yacymodel.Hash, 0, len(candidates))
	for _, c := range candidates {
		hashes = append(hashes, c.hash)
	}

	return hashes
}

func truncate(hashes []yacymodel.Hash, maxResults int) []yacymodel.Hash {
	if maxResults > 0 && len(hashes) > maxResults {
		return hashes[:maxResults]
	}

	return hashes
}

func candidateHashes(candidates map[yacymodel.Hash]candidate) []yacymodel.Hash {
	hashes := make([]yacymodel.Hash, 0, len(candidates))
	for hash := range candidates {
		hashes = append(hashes, hash)
	}

	return hashes
}

func largestCandidateSet(
	sets map[yacymodel.Hash]map[yacymodel.Hash]candidate,
) (yacymodel.Hash, bool) {
	var (
		selected yacymodel.Hash
		size     int
		ok       bool
	)
	for word, set := range sets {
		if !ok || len(set) > size || len(set) == size && compareAsc(word, selected) < 0 {
			selected = word
			size = len(set)
			ok = true
		}
	}

	return selected, ok
}

func compareDesc[T ~uint64](a, b T) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func compareAsc[T ~uint64 | ~string](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
