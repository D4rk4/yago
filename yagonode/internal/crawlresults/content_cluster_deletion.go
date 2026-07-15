package crawlresults

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
)

type contentClusterTransitionDeleter interface {
	contentClusterTransitionFinalizer
	DeleteTransition(context.Context, string) (contentcluster.EvidenceDeletion, error)
}

func (c *IngestConsumer) deleteDocumentClusterTransition(
	ctx context.Context,
	url string,
	transitions contentClusterTransitionDeleter,
) error {
	deletion, err := transitions.DeleteTransition(ctx, url)
	if err != nil {
		return fmt.Errorf("delete removed content cluster: %w", err)
	}
	projection := documentClusterProjection{
		finalizations: []contentcluster.EvidenceFinalization{deletion.Finalization},
		transitions:   transitions,
		replay:        deletion.Replay,
	}
	defer projection.release()

	documents, err := c.survivingDocumentClusterProjection(ctx, deletion)
	if err != nil {
		return err
	}
	if err := c.storeSurvivingDocumentClusterProjection(
		ctx,
		documents,
		deletion.Replay,
	); err != nil {
		return err
	}
	if err := projection.finalize(ctx); err != nil {
		return err
	}

	return nil
}
