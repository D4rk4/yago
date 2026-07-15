package yagonode

import (
	"context"
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type contentClusterDocumentTransition interface {
	contentClusterDocumentEvictor
	DeleteTransition(context.Context, string) (contentcluster.EvidenceDeletion, error)
	FinalizeEvidenceTransitions(
		context.Context,
		[]contentcluster.EvidenceFinalization,
	) error
	ReleaseEvidenceTransitions([]contentcluster.EvidenceFinalization)
}

type documentContentClusterDeletion struct {
	updates       []documentstore.Document
	clusterIDs    []string
	finalizations []contentcluster.EvidenceFinalization
	transitions   contentClusterDocumentTransition
	replay        bool
}

func (d documentLineageEvictor) prepareContentClusterDeletion(
	ctx context.Context,
	normalizedURL string,
	sourceDocument documentstore.Document,
	sourceFound bool,
) (documentContentClusterDeletion, error) {
	deletion, err := d.beginContentClusterDeletion(ctx, normalizedURL)
	if err != nil {
		return documentContentClusterDeletion{}, err
	}

	projected, err := d.projectContentClusterDeletion(
		ctx,
		deletion,
		normalizedURL,
		sourceDocument,
		sourceFound,
	)
	if err != nil {
		deletion.release()
	}

	return projected, err
}

func (d documentLineageEvictor) beginContentClusterDeletion(
	ctx context.Context,
	normalizedURL string,
) (documentContentClusterDeletion, error) {
	if d.clusters == nil {
		return documentContentClusterDeletion{}, nil
	}
	transitions, durable := d.clusters.(contentClusterDocumentTransition)
	if !durable {
		assignment, found, err := d.clusters.Lookup(ctx, normalizedURL)
		if err != nil {
			return documentContentClusterDeletion{}, fmt.Errorf(
				"look up document content cluster: %w",
				err,
			)
		}
		if _, err := d.clusters.Delete(ctx, normalizedURL); err != nil {
			return documentContentClusterDeletion{}, fmt.Errorf(
				"delete document content cluster: %w",
				err,
			)
		}
		deletion := documentContentClusterDeletion{replay: true}
		if found && assignment.ClusterID != "" {
			deletion.clusterIDs = []string{assignment.ClusterID}
		}

		return deletion, nil
	}
	deletion, err := transitions.DeleteTransition(ctx, normalizedURL)
	if err != nil {
		return documentContentClusterDeletion{}, fmt.Errorf(
			"delete document content cluster: %w",
			err,
		)
	}
	projection := documentContentClusterDeletion{
		finalizations: []contentcluster.EvidenceFinalization{deletion.Finalization},
		transitions:   transitions,
		replay:        deletion.Replay,
		clusterIDs:    append([]string(nil), deletion.AffectedClusterIDs...),
	}
	sort.Strings(projection.clusterIDs)

	return projection, nil
}

func (d documentLineageEvictor) projectContentClusterDeletion(
	ctx context.Context,
	projection documentContentClusterDeletion,
	normalizedURL string,
	sourceDocument documentstore.Document,
	sourceFound bool,
) (documentContentClusterDeletion, error) {
	if d.clusters == nil {
		return projection, nil
	}
	updates := make(map[string]documentstore.Document)
	for _, clusterID := range contentClusterDeletionIDs(projection, sourceDocument, sourceFound) {
		if err := d.projectSurvivingContentCluster(
			ctx,
			clusterID,
			normalizedURL,
			projection.replay,
			updates,
		); err != nil {
			return documentContentClusterDeletion{}, err
		}
	}
	projection.updates = sortedContentClusterDocuments(updates)

	return projection, nil
}

func (d documentContentClusterDeletion) finalize(ctx context.Context) error {
	if d.transitions == nil {
		return nil
	}
	if err := d.transitions.FinalizeEvidenceTransitions(ctx, d.finalizations); err != nil {
		return fmt.Errorf("finalize document content cluster: %w", err)
	}

	return nil
}

func (d documentContentClusterDeletion) release() {
	if d.transitions != nil {
		d.transitions.ReleaseEvidenceTransitions(d.finalizations)
	}
}
