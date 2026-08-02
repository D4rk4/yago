package contentcluster

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (i *Index) publishedCluster(
	tx *vault.Txn,
	ctx context.Context,
	clusterID string,
) (clusterRecord, bool, error) {
	return i.resolveCluster(tx, ctx, clusterID, i.publishedFingerprint)
}

func (i *Index) projectedCluster(
	tx *vault.Txn,
	ctx context.Context,
	clusterID string,
) (clusterRecord, bool, error) {
	return i.resolveCluster(tx, ctx, clusterID, i.projectedFingerprint)
}

func (i *Index) resolveCluster(
	tx *vault.Txn,
	ctx context.Context,
	clusterID string,
	resolve func(*vault.Txn, string) (fingerprintRecord, bool, error),
) (clusterRecord, bool, error) {
	cluster, found, err := i.clusters.Get(tx, vault.Key(clusterID))
	if err != nil || !found {
		if err != nil {
			return clusterRecord{}, false, fmt.Errorf("read content cluster projection: %w", err)
		}

		return clusterRecord{}, false, nil
	}
	if len(cluster.Members) > i.limits.MaximumClusterMembers {
		return clusterRecord{}, false, fmt.Errorf("content cluster exceeds its member limit")
	}
	members := make([]string, 0, len(cluster.Members))
	var representative representativeRecord
	for _, url := range cluster.Members {
		if err := ctx.Err(); err != nil {
			return clusterRecord{}, false, fmt.Errorf("resolve content cluster: %w", err)
		}
		record, visible, err := resolve(tx, url)
		if err != nil {
			return clusterRecord{}, false, err
		}
		if !visible || record.ClusterID != clusterID {
			continue
		}
		members = insertSorted(members, url)
		candidate := representativeFrom(record)
		if len(members) == 1 || betterRepresentative(candidate, representative) {
			representative = candidate
		}
	}
	if len(members) == 0 {
		return clusterRecord{}, false, nil
	}
	return clusterRecord{
		ID:             clusterID,
		Members:        members,
		Representative: representative,
	}, true, nil
}

func (i *Index) publishedFingerprint(
	tx *vault.Txn,
	url string,
) (fingerprintRecord, bool, error) {
	return i.fingerprints.Get(tx, vault.Key(url))
}

func (i *Index) projectedFingerprint(
	tx *vault.Txn,
	url string,
) (fingerprintRecord, bool, error) {
	transition, found, err := i.fingerprints.transition(tx, url)
	if err != nil {
		return fingerprintRecord{}, false, err
	}
	if found {
		return transition.Current, transition.CurrentFound, nil
	}

	return i.publishedFingerprint(tx, url)
}

// projectedFingerprintMatch is projectedFingerprint for the posting path: same
// precedence of an in-flight transition over the published entry, same absence,
// but reading only the fields postingMatches compares.
func (i *Index) projectedFingerprintMatch(
	tx *vault.Txn,
	url string,
) (fingerprintMatch, bool, error) {
	transition, found, err := i.fingerprints.transitionMatch(tx, url)
	if err != nil {
		return fingerprintMatch{}, false, err
	}
	if found {
		return transition.Current, transition.CurrentFound, nil
	}

	return i.fingerprints.match(tx, vault.Key(url))
}

func (i *Index) attachProjectedCluster(
	tx *vault.Txn,
	ctx context.Context,
	record fingerprintRecord,
) error {
	cluster, found, err := i.projectedCluster(tx, ctx, record.ClusterID)
	if err != nil {
		return fmt.Errorf("read projected content cluster: %w", err)
	}
	if !found {
		cluster = clusterRecord{ID: record.ClusterID}
	}
	cluster.Members = insertSorted(cluster.Members, record.URL)
	if len(cluster.Members) > i.limits.MaximumClusterMembers {
		return fmt.Errorf("content cluster %q reached its member limit", record.ClusterID)
	}
	// resolveCluster already chose the best representative among the members it
	// could see, and normalizeProjectedCluster stores that choice unchanged, so
	// it is authoritative. The only member it cannot have seen is the record
	// being attached. Folding that one record in replaces a second full pass
	// over the cluster: at up to MaximumClusterMembers members each carrying up
	// to MaximumShingles shingles, re-reading every member to re-derive four
	// scalars was the dominant cost of absorbing a page.
	attached, visible, err := i.projectedFingerprint(tx, record.URL)
	if err != nil {
		return fmt.Errorf("read projected fingerprint: %w", err)
	}
	if !visible {
		return fmt.Errorf("content fingerprint %q is missing", record.URL)
	}
	candidate := representativeFrom(attached)
	if !found || betterRepresentative(candidate, cluster.Representative) {
		cluster.Representative = candidate
	}
	if err := i.clusters.Put(tx, vault.Key(cluster.ID), cluster); err != nil {
		return fmt.Errorf("store projected content cluster: %w", err)
	}

	return nil
}

func (i *Index) normalizeProjectedCluster(
	tx *vault.Txn,
	ctx context.Context,
	clusterID string,
) error {
	if clusterID == "" {
		return nil
	}
	cluster, found, err := i.projectedCluster(tx, ctx, clusterID)
	if err != nil {
		return fmt.Errorf("read normalized content cluster: %w", err)
	}
	if !found {
		if _, err := i.clusters.Delete(tx, vault.Key(clusterID)); err != nil {
			return fmt.Errorf("delete empty content cluster: %w", err)
		}

		return nil
	}
	if err := i.clusters.Put(tx, vault.Key(clusterID), cluster); err != nil {
		return fmt.Errorf("store normalized content cluster: %w", err)
	}

	return nil
}
