package documentstore

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type outboundAnchorPublication struct {
	Targets  []string
	Revision string
}

type outboundAnchorLease struct {
	once     sync.Once
	released atomic.Bool
	release  func()
	targets  []string
}

func newOutboundAnchorLease(
	release func(),
	targets []string,
) *outboundAnchorLease {
	return &outboundAnchorLease{
		release: release,
		targets: append([]string(nil), targets...),
	}
}

func (l *outboundAnchorLease) close() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.released.Store(true)
		l.release()
	})
}

func (d documentVault) readOutboundAnchorPublication(
	tx *vault.Txn,
	sourceURL string,
) (outboundAnchorPublication, error) {
	publication, found, err := d.outboundPublications.Get(tx, vault.Key(sourceURL))
	if err != nil {
		return outboundAnchorPublication{}, fmt.Errorf(
			"read outbound anchor publication: %w",
			err,
		)
	}
	if found {
		return canonicalOutboundAnchorPublication(publication), nil
	}
	targets, _, err := d.outboundTargets.Get(tx, vault.Key(sourceURL))
	if err != nil {
		return outboundAnchorPublication{}, fmt.Errorf(
			"read legacy outbound targets: %w",
			err,
		)
	}

	return outboundAnchorPublication{Targets: uniqueSortedStrings(targets)}, nil
}

func (d documentVault) FinalizeOutboundAnchors(
	ctx context.Context,
	finalizations []OutboundAnchorFinalization,
) error {
	if len(finalizations) == 0 {
		return nil
	}
	defer d.ReleaseOutboundAnchors(finalizations)
	ordered, err := canonicalOutboundAnchorFinalizations(finalizations)
	if err != nil {
		return err
	}
	if err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, finalization := range ordered {
			current, err := d.readOutboundAnchorPublication(tx, finalization.sourceURL)
			if err != nil {
				return err
			}
			if outboundAnchorPublicationsEqual(current, finalization.desired) {
				continue
			}
			if !outboundAnchorPublicationsEqual(current, finalization.expected) {
				return fmt.Errorf("outbound anchor publication changed before finalization")
			}
			if err := d.outboundPublications.Put(
				tx,
				vault.Key(finalization.sourceURL),
				finalization.desired,
			); err != nil {
				return fmt.Errorf("store outbound anchor publication: %w", err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("finalize outbound anchors: %w", err)
	}

	return nil
}

func (d documentVault) ReleaseOutboundAnchors(
	finalizations []OutboundAnchorFinalization,
) {
	for _, finalization := range finalizations {
		finalization.lease.close()
	}
}

func canonicalOutboundAnchorFinalizations(
	finalizations []OutboundAnchorFinalization,
) ([]OutboundAnchorFinalization, error) {
	if len(finalizations) > maximumOutboundAnchorSources {
		return nil, fmt.Errorf("outbound anchor finalization source limit exceeded")
	}
	bySource := make(map[string]OutboundAnchorFinalization, len(finalizations))
	var lease *outboundAnchorLease
	for _, finalization := range finalizations {
		finalization.sourceURL = strings.TrimSpace(finalization.sourceURL)
		if finalization.sourceURL == "" {
			return nil, fmt.Errorf("outbound anchor finalization source must not be empty")
		}
		if finalization.lease == nil || finalization.lease.released.Load() {
			return nil, fmt.Errorf("outbound anchor finalization lease is not active")
		}
		if lease == nil {
			lease = finalization.lease
		} else if lease != finalization.lease {
			return nil, fmt.Errorf("outbound anchor finalizations do not share one lease")
		}
		finalization.expected = canonicalOutboundAnchorPublication(finalization.expected)
		finalization.desired = canonicalOutboundAnchorPublication(finalization.desired)
		if _, duplicate := bySource[finalization.sourceURL]; duplicate {
			return nil, fmt.Errorf("duplicate outbound anchor finalization source")
		}
		bySource[finalization.sourceURL] = finalization
	}
	sources := make([]string, 0, len(bySource))
	for sourceURL := range bySource {
		sources = append(sources, sourceURL)
	}
	sort.Strings(sources)
	ordered := make([]OutboundAnchorFinalization, 0, len(sources))
	for _, sourceURL := range sources {
		ordered = append(ordered, bySource[sourceURL])
	}

	return ordered, nil
}

func canonicalOutboundAnchorPublication(
	publication outboundAnchorPublication,
) outboundAnchorPublication {
	return outboundAnchorPublication{
		Targets:  uniqueSortedStrings(publication.Targets),
		Revision: publication.Revision,
	}
}

func outboundAnchorPublicationsEqual(
	left outboundAnchorPublication,
	right outboundAnchorPublication,
) bool {
	return left.Revision == right.Revision && slices.Equal(left.Targets, right.Targets)
}
