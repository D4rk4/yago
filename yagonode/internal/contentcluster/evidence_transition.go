package contentcluster

import (
	"crypto/rand"
	"errors"
	"slices"
)

var errEvidenceTransitionConflict = errors.New("content evidence transition conflict")

type EvidenceFinalization struct {
	url        string
	token      string
	urlLease   *evidenceLease
	candidate  *evidenceLease
	projection *evidenceLease
}

type EvidenceDeletion struct {
	Previous           Assignment
	PreviousFound      bool
	Deleted            bool
	Replay             bool
	AffectedClusterIDs []string
	Finalization       EvidenceFinalization
}

type fingerprintTransition struct {
	Token              string            `json:"token"`
	URL                string            `json:"url"`
	Previous           fingerprintRecord `json:"previous"`
	PreviousFound      bool              `json:"previous_found"`
	Current            fingerprintRecord `json:"current"`
	CurrentFound       bool              `json:"current_found"`
	PreviousAssignment Assignment        `json:"previous_assignment"`
	CurrentAssignment  Assignment        `json:"current_assignment"`
	AffectedClusterIDs []string          `json:"affected_cluster_ids"`
}

func newEvidenceFinalization(url string) EvidenceFinalization {
	return EvidenceFinalization{url: url, token: rand.Text()}
}

func (t fingerprintTransition) finalization() EvidenceFinalization {
	return EvidenceFinalization{url: t.URL, token: t.Token}
}

func (t fingerprintTransition) affectedClusterIDs() []string {
	return append([]string(nil), t.AffectedClusterIDs...)
}

func mergeAffectedClusterIDs(groups ...[]string) []string {
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, clusterID := range group {
			if clusterID != "" {
				seen[clusterID] = struct{}{}
			}
		}
	}
	clusterIDs := make([]string, 0, len(seen))
	for clusterID := range seen {
		clusterIDs = append(clusterIDs, clusterID)
	}
	slices.Sort(clusterIDs)

	return clusterIDs
}

func sameFingerprintRecord(left fingerprintRecord, right fingerprintRecord) bool {
	return left.URL == right.URL &&
		left.ContentHash == right.ContentHash &&
		left.Fingerprint == right.Fingerprint &&
		slices.Equal(left.Shingles, right.Shingles) &&
		left.ClusterID == right.ClusterID &&
		left.CanonicalPreferred == right.CanonicalPreferred &&
		left.Quality == right.Quality &&
		left.InboundAuthority == right.InboundAuthority
}
