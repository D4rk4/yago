package contentcluster

import (
	"context"
	"fmt"
)

const (
	contentHashCandidatePrefix = "content-hash\x00"
	fingerprintBandPrefix      = "fingerprint-band\x00"
)

func (i *Index) acquireCandidateLease(
	ctx context.Context,
	prepared []preparedEvidence,
) (*evidenceLease, error) {
	lease, err := i.candidateBoundaries.acquireLease(
		ctx,
		candidateBoundaryIdentities(prepared),
	)
	if err != nil {
		return nil, fmt.Errorf("acquire content candidate lease: %w", err)
	}

	return lease, nil
}

func (i *Index) acquirePreparedReplacementLeases(
	ctx context.Context,
	urls []string,
	prepared []preparedEvidence,
) (*replacementLeases, error) {
	urlLease, err := i.boundaries.acquireLease(ctx, urls)
	if err != nil {
		return nil, fmt.Errorf("acquire evidence boundaries: %w", err)
	}
	candidate, err := i.acquireCandidateLease(ctx, prepared)
	if err != nil {
		urlLease.close()

		return nil, err
	}
	identities, err := i.evidenceProjectionIdentities(ctx, urls)
	if err != nil {
		candidate.close()
		urlLease.close()

		return nil, err
	}
	projection, err := i.projections.acquireLease(ctx, identities)
	if err != nil {
		candidate.close()
		urlLease.close()

		return nil, fmt.Errorf("acquire evidence projection lease: %w", err)
	}

	return &replacementLeases{
		url:        urlLease,
		candidate:  candidate,
		projection: projection,
		identities: identities,
	}, nil
}

func candidateBoundaryIdentities(prepared []preparedEvidence) []string {
	identities := make([]string, 0, len(prepared)*(bandCount+1))
	for _, item := range prepared {
		identities = append(identities, contentHashCandidatePrefix+item.ContentHash)
		if len(item.Shingles) == 0 {
			continue
		}
		for band, value := range item.Bands {
			identity := []byte(fingerprintBandPrefix)
			identity = append(identity, byte(band), value)
			identities = append(identities, string(identity))
		}
	}

	return identities
}
