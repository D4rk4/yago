package contentcluster

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var errInvalidFingerprintClusterIdentity = errors.New(
	"content fingerprint cluster identity is invalid",
)

type replacementBatchProjection struct {
	current  []fingerprintRecord
	previous []fingerprintRecord
}

func (i *Index) publishedRecordCluster(
	tx *vault.Txn,
	ctx context.Context,
	record fingerprintRecord,
) (clusterRecord, bool, error) {
	if strings.TrimSpace(record.ClusterID) == "" {
		return clusterRecord{}, false, errInvalidFingerprintClusterIdentity
	}
	cluster, found, err := i.publishedCluster(tx, ctx, record.ClusterID)
	if err != nil {
		return clusterRecord{}, false, fmt.Errorf("read existing content cluster: %w", err)
	}
	if !found {
		return clusterRecord{}, false, nil
	}
	_, member := slices.BinarySearch(cluster.Members, record.URL)

	return cluster, member, nil
}

func (i *Index) recoveredMembershipClusterID(
	record fingerprintRecord,
	cluster clusterRecord,
	planned replacementBatchProjection,
) string {
	members := clusterMembershipAfterEarlierPlans(
		cluster,
		record.ClusterID,
		planned,
	)
	if len(members) < i.limits.MaximumClusterMembers {
		return record.ClusterID
	}

	return stableClusterID(
		record.URL,
		record.ContentHash+"\x00cluster-membership-recovery\x00"+record.ClusterID,
	)
}

func clusterMembershipAfterEarlierPlans(
	cluster clusterRecord,
	clusterID string,
	planned replacementBatchProjection,
) map[string]struct{} {
	members := make(
		map[string]struct{},
		len(cluster.Members)+len(planned.current),
	)
	for _, member := range cluster.Members {
		members[member] = struct{}{}
	}
	for _, candidate := range planned.previous {
		if candidate.ClusterID == clusterID {
			delete(members, candidate.URL)
		}
	}
	for _, candidate := range planned.current {
		if candidate.ClusterID == clusterID {
			members[candidate.URL] = struct{}{}
		}
	}

	return members
}

func sameClusterContent(record fingerprintRecord, prepared preparedEvidence) bool {
	return record.URL == prepared.URL &&
		record.ContentHash == prepared.ContentHash &&
		record.Fingerprint == prepared.Fingerprint &&
		slices.Equal(record.Shingles, prepared.Shingles)
}

func newReplacementTransition(
	previous fingerprintRecord,
	previousFound bool,
	previousAssignment Assignment,
	current fingerprintRecord,
	pending fingerprintTransition,
) fingerprintTransition {
	transition := fingerprintTransition{
		Token:              newEvidenceFinalization(current.URL).token,
		URL:                current.URL,
		Previous:           previous,
		PreviousFound:      previousFound,
		Current:            current,
		CurrentFound:       true,
		PreviousAssignment: previousAssignment,
	}
	transition.AffectedClusterIDs = mergeAffectedClusterIDs(
		pending.AffectedClusterIDs,
		[]string{previous.ClusterID, current.ClusterID},
	)

	return transition
}

func transitionPreviousAssignmentFound(transition fingerprintTransition) bool {
	return transition.PreviousFound && transition.PreviousAssignment.ClusterID != ""
}
