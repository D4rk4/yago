package crawlresults

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type contentClusterTransitionFinalizer interface {
	FinalizeEvidenceTransitions(
		context.Context,
		[]contentcluster.EvidenceFinalization,
	) error
	ReleaseEvidenceTransitions([]contentcluster.EvidenceFinalization)
}

type documentClusterProjection struct {
	documents     []documentstore.Document
	finalizations []contentcluster.EvidenceFinalization
	transitions   contentClusterTransitionFinalizer
	replay        bool
}

func (p documentClusterProjection) finalize(ctx context.Context) error {
	if p.transitions == nil {
		return nil
	}
	if err := p.transitions.FinalizeEvidenceTransitions(ctx, p.finalizations); err != nil {
		return fmt.Errorf("finalize content cluster projection: %w", err)
	}

	return nil
}

func (p documentClusterProjection) release() {
	if p.transitions != nil {
		p.transitions.ReleaseEvidenceTransitions(p.finalizations)
	}
}
