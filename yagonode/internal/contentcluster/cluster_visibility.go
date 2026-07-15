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
	cluster.Representative, err = i.projectedRepresentative(tx, ctx, cluster.Members)
	if err != nil {
		return err
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

func (i *Index) projectedRepresentative(
	tx *vault.Txn,
	ctx context.Context,
	members []string,
) (representativeRecord, error) {
	var representative representativeRecord
	for position, url := range members {
		if err := ctx.Err(); err != nil {
			return representativeRecord{}, fmt.Errorf(
				"choose projected representative: %w",
				err,
			)
		}
		record, found, err := i.projectedFingerprint(tx, url)
		if err != nil {
			return representativeRecord{}, fmt.Errorf("read projected fingerprint: %w", err)
		}
		if !found {
			return representativeRecord{}, fmt.Errorf("content fingerprint %q is missing", url)
		}
		candidate := representativeFrom(record)
		if position == 0 || betterRepresentative(candidate, representative) {
			representative = candidate
		}
	}

	return representative, nil
}
