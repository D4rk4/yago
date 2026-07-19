package contentcluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/bits"
	"slices"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type candidateMatch struct {
	record     fingerprintRecord
	similarity float64
	distance   int
}

func (i *Index) findMatch(
	tx *vault.Txn,
	ctx context.Context,
	prepared preparedEvidence,
) (candidateMatch, bool, error) {
	exactURLs, err := i.postingURLs(tx, i.exactBuckets, vault.Key(prepared.ContentHash))
	if err != nil {
		return candidateMatch{}, false, err
	}
	best, found, err := i.bestCandidate(tx, ctx, prepared, exactURLs, true)
	if err != nil || found {
		return best, found, err
	}
	if len(prepared.Shingles) == 0 {
		return candidateMatch{}, false, nil
	}
	candidates := make([]string, 0, i.limits.MaximumCandidates)
	seen := make(map[string]struct{}, i.limits.MaximumCandidates)
	for band, value := range prepared.Bands {
		if err := ctx.Err(); err != nil {
			return candidateMatch{}, false, fmt.Errorf("check candidate context: %w", err)
		}
		urls, readErr := i.postingURLs(
			tx,
			i.bandBuckets,
			bandKey(uint8(band), value),
		)
		if readErr != nil {
			return candidateMatch{}, false, readErr
		}
		for _, url := range urls {
			if _, exists := seen[url]; exists {
				continue
			}
			seen[url] = struct{}{}
			candidates = append(candidates, url)
			if len(candidates) == i.limits.MaximumCandidates {
				break
			}
		}
		if len(candidates) == i.limits.MaximumCandidates {
			break
		}
	}

	return i.bestCandidate(tx, ctx, prepared, candidates, false)
}

func (i *Index) bestCandidate(
	tx *vault.Txn,
	ctx context.Context,
	prepared preparedEvidence,
	urls []string,
	exact bool,
) (candidateMatch, bool, error) {
	var best candidateMatch
	found := false
	for position, url := range urls {
		if position == i.limits.MaximumCandidates {
			break
		}
		if err := ctx.Err(); err != nil {
			return candidateMatch{}, false, fmt.Errorf("check verification context: %w", err)
		}
		candidate, eligible, err := i.candidate(tx, ctx, prepared, url, exact)
		if err != nil {
			return candidateMatch{}, false, err
		}
		if !eligible {
			continue
		}
		if !found || betterCandidate(candidate, best) {
			best = candidate
			found = true
		}
	}

	return best, found, nil
}

func (i *Index) candidate(
	tx *vault.Txn,
	ctx context.Context,
	prepared preparedEvidence,
	url string,
	exact bool,
) (candidateMatch, bool, error) {
	record, exists, err := i.fingerprints.Get(tx, vault.Key(url))
	if err != nil {
		return candidateMatch{}, false, fmt.Errorf("read candidate fingerprint: %w", err)
	}
	if !exists || record.URL == prepared.URL {
		return candidateMatch{}, false, nil
	}
	cluster, exists, err := i.publishedRecordCluster(tx, ctx, record)
	if err != nil {
		return candidateMatch{}, false, fmt.Errorf("read candidate content cluster: %w", err)
	}
	if !exists || len(cluster.Members) >= i.limits.MaximumClusterMembers {
		return candidateMatch{}, false, nil
	}
	if exact && record.ContentHash != prepared.ContentHash {
		return candidateMatch{}, false, nil
	}
	similarity := 1.0
	if !exact {
		similarity = boundedJaccard(prepared.Shingles, record.Shingles)
		if similarity < i.limits.MinimumJaccard {
			return candidateMatch{}, false, nil
		}
	}

	return candidateMatch{
		record:     record,
		similarity: similarity,
		distance:   bits.OnesCount64(prepared.Fingerprint ^ record.Fingerprint),
	}, true, nil
}

func betterCandidate(left, right candidateMatch) bool {
	if left.similarity != right.similarity {
		return left.similarity > right.similarity
	}
	if left.distance != right.distance {
		return left.distance < right.distance
	}
	if betterRepresentative(representativeFrom(left.record), representativeFrom(right.record)) {
		return true
	}
	if betterRepresentative(representativeFrom(right.record), representativeFrom(left.record)) {
		return false
	}

	return left.record.ClusterID < right.record.ClusterID
}

func betterRepresentative(left, right representativeRecord) bool {
	if left.CanonicalPreferred != right.CanonicalPreferred {
		return left.CanonicalPreferred
	}
	if left.Quality != right.Quality {
		return left.Quality > right.Quality
	}
	if left.InboundAuthority != right.InboundAuthority {
		return left.InboundAuthority > right.InboundAuthority
	}

	return left.URL < right.URL
}

func (i *Index) postingURLs(
	tx *vault.Txn,
	collection *vault.Keyspace[postingRecord],
	key vault.Key,
) ([]string, error) {
	posting, found, err := collection.Get(tx, key)
	if err != nil || !found {
		if err != nil {
			return nil, fmt.Errorf("read content candidate bucket: %w", err)
		}

		return nil, nil
	}
	if len(posting.URLs) > i.limits.MaximumBucketMembers {
		return nil, fmt.Errorf("content candidate bucket exceeds its member limit")
	}

	return posting.URLs, nil
}

func sameEvidence(record fingerprintRecord, prepared preparedEvidence) bool {
	return record.URL == prepared.URL &&
		record.ContentHash == prepared.ContentHash &&
		record.Fingerprint == prepared.Fingerprint &&
		slices.Equal(record.Shingles, prepared.Shingles) &&
		record.CanonicalPreferred == prepared.CanonicalPreferred &&
		record.Quality == prepared.Quality &&
		record.InboundAuthority == prepared.InboundAuthority
}

func insertSorted(values []string, value string) []string {
	position, exists := slices.BinarySearch(values, value)
	if exists {
		return values
	}
	values = append(values, "")
	copy(values[position+1:], values[position:])
	values[position] = value

	return values
}

func stableClusterID(url string, contentHash string) string {
	identity := sha256.Sum256([]byte(url + "\x00" + contentHash))

	return "content-" + hex.EncodeToString(identity[:])
}

func bandKey(band uint8, value uint8) vault.Key {
	return vault.Key{band, value}
}
