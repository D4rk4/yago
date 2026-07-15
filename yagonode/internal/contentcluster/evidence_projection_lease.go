package contentcluster

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (i *Index) evidenceProjectionIdentities(
	ctx context.Context,
	urls []string,
) ([]string, error) {
	identities := make([]string, 0, len(urls)*2)
	err := i.vault.View(ctx, func(tx *vault.Txn) error {
		for _, url := range urls {
			transition, found, err := i.fingerprints.transition(tx, url)
			if err != nil {
				return err
			}
			if found {
				identities = append(identities, transition.AffectedClusterIDs...)
			}
			record, found, err := i.fingerprints.Get(tx, vault.Key(url))
			if err != nil {
				return err
			}
			if found {
				identities = append(identities, record.ClusterID)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read evidence projection identities: %w", err)
	}

	return mergeAffectedClusterIDs(identities), nil
}

func transitionProjectionIdentities(transitions []fingerprintTransition) []string {
	identities := make([]string, 0, len(transitions)*2)
	for _, transition := range transitions {
		identities = append(identities, transition.AffectedClusterIDs...)
	}

	return mergeAffectedClusterIDs(identities)
}

func replacementProjectionIdentities(replacements []EvidenceReplacement) []string {
	identities := make([]string, 0, len(replacements)*2)
	for _, replacement := range replacements {
		identities = append(identities, replacement.AffectedClusterIDs...)
	}

	return mergeAffectedClusterIDs(identities)
}

func attachEvidenceLease(
	replacements []EvidenceReplacement,
	urlLease *evidenceLease,
	candidate *evidenceLease,
	projection *evidenceLease,
) bool {
	attached := false
	for position := range replacements {
		if replacements[position].Finalization.token == "" {
			continue
		}
		replacements[position].Finalization.urlLease = urlLease
		replacements[position].Finalization.candidate = candidate
		replacements[position].Finalization.projection = projection
		attached = true
	}

	return attached
}

func releaseEvidenceLeases(finalizations []EvidenceFinalization) {
	seen := make(map[*evidenceLease]struct{}, len(finalizations)*2)
	for _, finalization := range finalizations {
		for _, lease := range []*evidenceLease{
			finalization.projection,
			finalization.candidate,
			finalization.urlLease,
		} {
			if lease == nil {
				continue
			}
			if _, found := seen[lease]; found {
				continue
			}
			seen[lease] = struct{}{}
			lease.close()
		}
	}
}
