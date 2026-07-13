package crawlresults

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type contentClusterBatch interface {
	ReplaceBatch(
		context.Context,
		[]contentcluster.Evidence,
	) ([]contentcluster.EvidenceReplacement, error)
}

func (c *IngestConsumer) replaceDocumentClusterBatch(
	ctx context.Context,
	docs []documentstore.Document,
	clusters contentClusterBatch,
) (documentClusterReplacement, error) {
	evidence := make([]contentcluster.Evidence, len(docs))
	for position, doc := range docs {
		evidence[position] = documentClusterEvidence(doc)
	}
	replacements, err := clusters.ReplaceBatch(ctx, evidence)
	if err != nil {
		return documentClusterReplacement{}, fmt.Errorf("replace content clusters: %w", err)
	}
	if len(replacements) != len(docs) {
		return documentClusterReplacement{}, fmt.Errorf(
			"content cluster replacements = %d, want %d",
			len(replacements),
			len(docs),
		)
	}

	replacement := newDocumentClusterReplacement(docs)
	for position, doc := range docs {
		transition := replacements[0]
		replacements = replacements[1:]
		if transition.PreviousFound {
			replacement.affectedClusterIDs[transition.Previous.ClusterID] = struct{}{}
		}
		assigned := assignedDocumentCluster(doc, transition.Current)
		replacement.documents[position] = assigned
		replacement.currentURLs[assigned.NormalizedURL] = struct{}{}
		replacement.affectedClusterIDs[assigned.ClusterID] = struct{}{}
		replacement.assignedClusterIDs[assigned.ClusterID] = struct{}{}
	}

	return replacement, nil
}
