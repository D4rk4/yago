package yagonode

import (
	"context"
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func contentClusterDeletionIDs(
	projection documentContentClusterDeletion,
	sourceDocument documentstore.Document,
	sourceFound bool,
) []string {
	clusterIDs := append([]string(nil), projection.clusterIDs...)
	if len(clusterIDs) == 0 && sourceFound && sourceDocument.ClusterID != "" {
		clusterIDs = append(clusterIDs, sourceDocument.ClusterID)
	}
	sort.Strings(clusterIDs)

	return clusterIDs
}

func (d documentLineageEvictor) projectSurvivingContentCluster(
	ctx context.Context,
	clusterID string,
	normalizedURL string,
	replay bool,
	updates map[string]documentstore.Document,
) error {
	cluster, found, err := d.clusters.Cluster(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("read surviving document content cluster: %w", err)
	}
	if !found || d.directory == nil {
		return nil
	}
	for _, memberURL := range cluster.MemberURLs {
		if memberURL == normalizedURL {
			continue
		}
		document, found, err := d.contentClusterDocumentRevision(ctx, memberURL)
		if err != nil {
			return fmt.Errorf("read surviving clustered document: %w", err)
		}
		if !found || !replay && document.ClusterID == cluster.ID &&
			document.RepresentativeURL == cluster.RepresentativeURL {
			continue
		}
		document.ClusterID = cluster.ID
		document.RepresentativeURL = cluster.RepresentativeURL
		updates[document.NormalizedURL] = document
	}

	return nil
}

func sortedContentClusterDocuments(
	updates map[string]documentstore.Document,
) []documentstore.Document {
	urls := make([]string, 0, len(updates))
	for url := range updates {
		urls = append(urls, url)
	}
	sort.Strings(urls)
	documents := make([]documentstore.Document, 0, len(urls))
	for _, url := range urls {
		documents = append(documents, updates[url])
	}

	return documents
}
