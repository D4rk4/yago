package contentcluster

import (
	"context"
	"errors"
	"fmt"
)

type EvidenceReplacement struct {
	Previous           Assignment
	PreviousFound      bool
	Current            Assignment
	Replay             bool
	AffectedClusterIDs []string
	Finalization       EvidenceFinalization
}

func (i *Index) ReplaceBatch(
	ctx context.Context,
	evidence []Evidence,
) ([]EvidenceReplacement, error) {
	prepared := make([]preparedEvidence, len(evidence))
	for position, item := range evidence {
		var err error
		prepared[position], err = prepareEvidence(ctx, i.limits, item)
		if err != nil {
			return nil, fmt.Errorf("prepare content cluster evidence %d: %w", position, err)
		}
	}
	if repeatedEvidenceURL(prepared) {
		replacements := make([]EvidenceReplacement, 0, len(prepared))
		for position, item := range prepared {
			current, err := i.replacePreparedBatch(ctx, []preparedEvidence{item})
			if err != nil {
				return nil, fmt.Errorf("replace content cluster evidence %d: %w", position, err)
			}
			replacements = append(replacements, current[0])
			if position < len(prepared)-1 {
				i.ReleaseEvidenceTransitions([]EvidenceFinalization{
					current[0].Finalization,
				})
			}
		}

		return replacements, nil
	}

	return i.replacePreparedBatch(ctx, prepared)
}

func (i *Index) replacePreparedBatch(
	ctx context.Context,
	prepared []preparedEvidence,
) ([]EvidenceReplacement, error) {
	if len(prepared) == 0 {
		return []EvidenceReplacement{}, nil
	}
	urls := make([]string, len(prepared))
	for position, item := range prepared {
		urls[position] = item.URL
	}
	leases, err := i.acquirePreparedReplacementLeases(ctx, urls, prepared)
	if err != nil {
		return nil, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			leases.close()
		}
	}()

	for {
		outputs, retry, err := i.executeReplacementAttempt(ctx, prepared, urls, leases)
		if err != nil {
			return nil, err
		}
		if retry {
			continue
		}
		if attachEvidenceLease(outputs, leases.url, leases.candidate, leases.projection) {
			handedOff = true
		}

		return outputs, nil
	}
}

func (i *Index) executeReplacementAttempt(
	ctx context.Context,
	prepared []preparedEvidence,
	urls []string,
	leases *replacementLeases,
) ([]EvidenceReplacement, bool, error) {
	attempt, err := i.buildReplacementAttempt(ctx, prepared, urls, leases.identities)
	if errors.Is(err, errEvidenceTransitionConflict) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	expanded, err := i.expandReplacementLeases(ctx, leases, attempt.identities)
	if err != nil || expanded {
		return nil, expanded, err
	}
	if err := i.commitReplacementPlans(ctx, attempt.plans); err != nil {
		if errors.Is(err, errEvidenceTransitionConflict) {
			return nil, true, nil
		}

		return nil, false, err
	}
	if err := i.completeReplacementOutputs(ctx, attempt.outputs); err != nil {
		return nil, false, err
	}

	return attempt.outputs, false, nil
}

type replacementLeases struct {
	url        *evidenceLease
	candidate  *evidenceLease
	projection *evidenceLease
	identities []string
}

type replacementAttempt struct {
	plans      []fingerprintTransition
	outputs    []EvidenceReplacement
	identities []string
}

func (i *Index) acquireReplacementLeases(
	ctx context.Context,
	urls []string,
) (*replacementLeases, error) {
	urlLease, err := i.boundaries.acquireLease(ctx, urls)
	if err != nil {
		return nil, fmt.Errorf("acquire evidence boundaries: %w", err)
	}
	identities, err := i.evidenceProjectionIdentities(ctx, urls)
	if err != nil {
		urlLease.close()

		return nil, err
	}
	projection, err := i.projections.acquireLease(ctx, identities)
	if err != nil {
		urlLease.close()

		return nil, fmt.Errorf("acquire evidence projection lease: %w", err)
	}

	return &replacementLeases{
		url:        urlLease,
		projection: projection,
		identities: identities,
	}, nil
}

func (l *replacementLeases) close() {
	l.projection.close()
	l.candidate.close()
	l.url.close()
}

func (i *Index) buildReplacementAttempt(
	ctx context.Context,
	prepared []preparedEvidence,
	urls []string,
	identities []string,
) (replacementAttempt, error) {
	pending, err := i.readTransitions(ctx, urls)
	if err != nil {
		return replacementAttempt{}, err
	}
	if err := i.reconcileTransitions(ctx, pending); err != nil {
		return replacementAttempt{}, err
	}
	plans, outputs, err := i.planReplacements(ctx, prepared, pending)
	if err != nil {
		return replacementAttempt{}, err
	}
	required := mergeAffectedClusterIDs(
		identities,
		transitionProjectionIdentities(pending),
		transitionProjectionIdentities(plans),
		replacementProjectionIdentities(outputs),
	)

	return replacementAttempt{plans: plans, outputs: outputs, identities: required}, nil
}

func (i *Index) expandReplacementLeases(
	ctx context.Context,
	leases *replacementLeases,
	required []string,
) (bool, error) {
	if leases.projection.covers(required) {
		return false, nil
	}
	leases.projection.close()
	projection, err := i.projections.acquireLease(ctx, required)
	if err != nil {
		return false, fmt.Errorf("expand evidence projection lease: %w", err)
	}
	leases.projection = projection
	leases.identities = required

	return true, nil
}

func (i *Index) commitReplacementPlans(
	ctx context.Context,
	plans []fingerprintTransition,
) error {
	if len(plans) == 0 {
		return nil
	}
	if err := i.persistTransitions(ctx, plans); err != nil {
		return err
	}

	return i.reconcileTransitions(ctx, plans)
}

func repeatedEvidenceURL(prepared []preparedEvidence) bool {
	seen := make(map[string]struct{}, len(prepared))
	for _, item := range prepared {
		if _, found := seen[item.URL]; found {
			return true
		}
		seen[item.URL] = struct{}{}
	}

	return false
}
