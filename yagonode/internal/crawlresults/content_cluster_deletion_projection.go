package crawlresults

import (
	"context"
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (c *IngestConsumer) survivingDocumentClusterProjection(
	ctx context.Context,
	deletion contentcluster.EvidenceDeletion,
) ([]documentstore.Document, error) {
	updates := make(map[string]documentstore.Document)
	clusterIDs := append([]string(nil), deletion.AffectedClusterIDs...)
	sort.Strings(clusterIDs)
	for _, clusterID := range clusterIDs {
		cluster, found, err := c.clusters.Cluster(ctx, clusterID)
		if err != nil {
			return nil, fmt.Errorf("read surviving content cluster: %w", err)
		}
		if !found {
			continue
		}
		documents, err := c.storedClusterProjection(ctx, cluster, nil, deletion.Replay)
		if err != nil {
			return nil, err
		}
		for _, document := range documents {
			updates[document.NormalizedURL] = document
		}
	}

	return orderedDocumentClusterProjection(updates), nil
}

func orderedDocumentClusterProjection(
	updates map[string]documentstore.Document,
) []documentstore.Document {
	urls := make([]string, 0, len(updates))
	for normalizedURL := range updates {
		urls = append(urls, normalizedURL)
	}
	sort.Strings(urls)
	documents := make([]documentstore.Document, 0, len(urls))
	for _, normalizedURL := range urls {
		documents = append(documents, updates[normalizedURL])
	}

	return documents
}

func (c *IngestConsumer) storeSurvivingDocumentClusterProjection(
	ctx context.Context,
	documents []documentstore.Document,
	replay bool,
) error {
	if len(documents) == 0 {
		return nil
	}
	receipt, err := c.documents.Receive(ctx, documents)
	if err != nil {
		return fmt.Errorf("store surviving content cluster: %w", err)
	}
	if receipt.Busy {
		return fmt.Errorf("store surviving content cluster at capacity")
	}
	committed := c.committedDocuments(receipt, documents)
	if replay {
		committed = documents
	}
	if c.index != nil {
		if err := c.indexDocuments(ctx, committed); err != nil {
			return fmt.Errorf("index surviving content cluster: %w", err)
		}
	}

	return nil
}
