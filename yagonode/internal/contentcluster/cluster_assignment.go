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

func (i *Index) replace(
	tx *vault.Txn,
	ctx context.Context,
	prepared preparedEvidence,
) (Assignment, error) {
	if err := ctx.Err(); err != nil {
		return Assignment{}, fmt.Errorf("check replacement context: %w", err)
	}
	previous, exists, err := i.fingerprints.Get(tx, vault.Key(prepared.URL))
	if err != nil {
		return Assignment{}, fmt.Errorf("read previous content fingerprint: %w", err)
	}
	if exists && sameEvidence(previous, prepared) {
		return i.existingAssignment(tx, previous.ClusterID)
	}
	if err := i.removePrevious(tx, ctx, previous, exists); err != nil {
		return Assignment{}, err
	}
	match, matched, err := i.findMatch(tx, ctx, prepared)
	if err != nil {
		return Assignment{}, err
	}
	clusterID := stableClusterID(prepared.URL, prepared.ContentHash)
	if matched {
		clusterID = match.record.ClusterID
	}
	record := recordFrom(prepared, clusterID)
	if err := i.persistFingerprint(tx, ctx, record, prepared.Bands); err != nil {
		return Assignment{}, err
	}

	return i.attachCluster(tx, ctx, record)
}

func (i *Index) existingAssignment(tx *vault.Txn, clusterID string) (Assignment, error) {
	cluster, found, err := i.clusters.Get(tx, vault.Key(clusterID))
	if err != nil {
		return Assignment{}, fmt.Errorf("read existing content cluster: %w", err)
	}
	if !found {
		return Assignment{}, fmt.Errorf("content cluster %q is missing", clusterID)
	}

	return assignmentFrom(cluster), nil
}

func (i *Index) removePrevious(
	tx *vault.Txn,
	ctx context.Context,
	previous fingerprintRecord,
	exists bool,
) error {
	if !exists {
		return nil
	}
	if err := i.detach(tx, ctx, previous); err != nil {
		return err
	}
	if _, err := i.fingerprints.Delete(tx, vault.Key(previous.URL)); err != nil {
		return fmt.Errorf("delete previous content fingerprint: %w", err)
	}

	return nil
}

func (i *Index) persistFingerprint(
	tx *vault.Txn,
	ctx context.Context,
	record fingerprintRecord,
	bands [bandCount]uint8,
) error {
	if err := i.fingerprints.Put(tx, vault.Key(record.URL), record); err != nil {
		return fmt.Errorf("store content fingerprint: %w", err)
	}
	if err := i.addPosting(
		tx,
		i.exactBuckets,
		vault.Key(record.ContentHash),
		record.URL,
	); err != nil {
		return err
	}
	if len(record.Shingles) == 0 {
		return nil
	}
	for band, value := range bands {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("check fingerprint band context: %w", err)
		}
		if err := i.addPosting(
			tx,
			i.bandBuckets,
			bandKey(uint8(band), value),
			record.URL,
		); err != nil {
			return err
		}
	}

	return nil
}

func (i *Index) attachCluster(
	tx *vault.Txn,
	ctx context.Context,
	record fingerprintRecord,
) (Assignment, error) {
	clusterID := record.ClusterID
	cluster, found, err := i.clusters.Get(tx, vault.Key(clusterID))
	if err != nil {
		return Assignment{}, fmt.Errorf("read target content cluster: %w", err)
	}
	if !found {
		cluster = clusterRecord{ID: clusterID}
	}
	cluster.Members = insertSorted(cluster.Members, record.URL)
	if len(cluster.Members) > i.limits.MaximumClusterMembers {
		return Assignment{}, fmt.Errorf("content cluster %q reached its member limit", clusterID)
	}
	representative, err := i.chooseRepresentative(tx, ctx, cluster.Members)
	if err != nil {
		return Assignment{}, err
	}
	cluster.Representative = representative
	if err := i.clusters.Put(tx, vault.Key(cluster.ID), cluster); err != nil {
		return Assignment{}, fmt.Errorf("store target content cluster: %w", err)
	}

	return assignmentFrom(cluster), nil
}

func (i *Index) detach(tx *vault.Txn, ctx context.Context, record fingerprintRecord) error {
	if err := i.removePosting(
		tx,
		i.exactBuckets,
		vault.Key(record.ContentHash),
		record.URL,
	); err != nil {
		return err
	}
	if len(record.Shingles) > 0 {
		for band, value := range fingerprintBands(record.Fingerprint) {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("check detached fingerprint context: %w", err)
			}
			if err := i.removePosting(
				tx,
				i.bandBuckets,
				bandKey(uint8(band), value),
				record.URL,
			); err != nil {
				return err
			}
		}
	}
	cluster, found, err := i.clusters.Get(tx, vault.Key(record.ClusterID))
	if err != nil {
		return fmt.Errorf("read detached content cluster: %w", err)
	}
	if !found {
		return fmt.Errorf("content cluster %q is missing", record.ClusterID)
	}
	cluster.Members = removeSorted(cluster.Members, record.URL)
	if len(cluster.Members) == 0 {
		_, err = i.clusters.Delete(tx, vault.Key(cluster.ID))
		if err != nil {
			return fmt.Errorf("delete empty content cluster: %w", err)
		}

		return nil
	}
	cluster.Representative, err = i.chooseRepresentative(tx, ctx, cluster.Members)
	if err != nil {
		return err
	}

	if err := i.clusters.Put(tx, vault.Key(cluster.ID), cluster); err != nil {
		return fmt.Errorf("store detached content cluster: %w", err)
	}

	return nil
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
		candidate, eligible, err := i.candidate(tx, prepared, url, exact)
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
	cluster, exists, err := i.clusters.Get(tx, vault.Key(record.ClusterID))
	if err != nil {
		return candidateMatch{}, false, fmt.Errorf("read candidate content cluster: %w", err)
	}
	if !exists || len(cluster.Members) >= i.limits.MaximumClusterMembers {
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

func (i *Index) chooseRepresentative(
	tx *vault.Txn,
	ctx context.Context,
	members []string,
) (representativeRecord, error) {
	if len(members) > i.limits.MaximumClusterMembers {
		return representativeRecord{}, fmt.Errorf("content cluster exceeds its member limit")
	}
	var representative representativeRecord
	for position, url := range members {
		if err := ctx.Err(); err != nil {
			return representativeRecord{}, fmt.Errorf("check representative context: %w", err)
		}
		record, exists, err := i.fingerprints.Get(tx, vault.Key(url))
		if err != nil {
			return representativeRecord{}, fmt.Errorf("read representative fingerprint: %w", err)
		}
		if !exists {
			return representativeRecord{}, fmt.Errorf("content fingerprint %q is missing", url)
		}
		candidate := representativeFrom(record)
		if position == 0 || betterRepresentative(candidate, representative) {
			representative = candidate
		}
	}

	return representative, nil
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

func (i *Index) addPosting(
	tx *vault.Txn,
	collection *vault.Collection[postingRecord],
	key vault.Key,
	url string,
) error {
	posting, _, err := collection.Get(tx, key)
	if err != nil {
		return fmt.Errorf("read content candidate posting: %w", err)
	}
	posting.URLs = insertSorted(posting.URLs, url)
	if len(posting.URLs) > i.limits.MaximumBucketMembers {
		posting.URLs = posting.URLs[:i.limits.MaximumBucketMembers]
	}

	if err := collection.Put(tx, key, posting); err != nil {
		return fmt.Errorf("store content candidate posting: %w", err)
	}

	return nil
}

func (i *Index) removePosting(
	tx *vault.Txn,
	collection *vault.Collection[postingRecord],
	key vault.Key,
	url string,
) error {
	posting, found, err := collection.Get(tx, key)
	if err != nil || !found {
		if err != nil {
			return fmt.Errorf("read removed content candidate posting: %w", err)
		}

		return nil
	}
	posting.URLs = removeSorted(posting.URLs, url)
	if len(posting.URLs) == 0 {
		_, err = collection.Delete(tx, key)
		if err != nil {
			return fmt.Errorf("delete empty content candidate posting: %w", err)
		}

		return nil
	}

	if err := collection.Put(tx, key, posting); err != nil {
		return fmt.Errorf("store removed content candidate posting: %w", err)
	}

	return nil
}

func (i *Index) postingURLs(
	tx *vault.Txn,
	collection *vault.Collection[postingRecord],
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

func removeSorted(values []string, value string) []string {
	position, exists := slices.BinarySearch(values, value)
	if !exists {
		return values
	}

	return append(values[:position], values[position+1:]...)
}

func stableClusterID(url string, contentHash string) string {
	identity := sha256.Sum256([]byte(url + "\x00" + contentHash))

	return "content-" + hex.EncodeToString(identity[:])
}

func bandKey(band uint8, value uint8) vault.Key {
	return vault.Key{band, value}
}
