package yagonode

import (
	"context"
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type contentClusterDocumentEvictor interface {
	Delete(context.Context, string) (bool, error)
	Lookup(context.Context, string) (contentcluster.Assignment, bool, error)
	Cluster(context.Context, string) (contentcluster.Cluster, bool, error)
}

type documentUpdateIndexer interface {
	Index(context.Context, documentstore.Document) error
}

type documentLineageEvictor struct {
	directory documentstore.DocumentDirectory
	receiver  documentstore.DocumentReceiver
	documents documentstore.DocumentEvictor
	anchors   documentstore.InboundAnchorReceiver
	clusters  contentClusterDocumentEvictor
	index     documentUpdateIndexer
}

func newDocumentLineageEvictor(storage nodeStorage) documentstore.DocumentEvictor {
	documents, _ := storage.documentDirectory.(documentstore.DocumentEvictor)
	if documents == nil {
		return nil
	}
	anchors, _ := storage.documentReceiver.(documentstore.InboundAnchorReceiver)

	return documentLineageEvictor{
		directory: storage.documentDirectory,
		receiver:  storage.documentReceiver,
		documents: documents,
		anchors:   anchors,
		clusters:  storage.contentClusters,
		index:     storage.searchIndex,
	}
}

func (d documentLineageEvictor) Delete(
	ctx context.Context,
	normalizedURL string,
) (bool, error) {
	updates := make(map[string]documentstore.Document)
	if d.anchors != nil {
		update, err := d.anchors.ReplaceOutboundAnchors(ctx, []documentstore.OutboundAnchorSet{{
			SourceURL: normalizedURL,
		}})
		if err != nil {
			return false, fmt.Errorf("clear outbound anchor contributions: %w", err)
		}
		if update.Busy {
			return false, fmt.Errorf("clear outbound anchor contributions at capacity")
		}
		for _, doc := range update.Documents {
			updates[doc.NormalizedURL] = doc
		}
	}

	clusterUpdates, err := d.deleteContentCluster(ctx, normalizedURL)
	if err != nil {
		return false, err
	}
	for _, doc := range clusterUpdates {
		updates[doc.NormalizedURL] = doc
	}

	if err := d.commitUpdates(ctx, updates); err != nil {
		return false, err
	}

	removed, err := d.documents.Delete(ctx, normalizedURL)
	if err != nil {
		return false, fmt.Errorf("delete stored document: %w", err)
	}

	return removed, nil
}

func (d documentLineageEvictor) deleteContentCluster(
	ctx context.Context,
	normalizedURL string,
) ([]documentstore.Document, error) {
	if d.clusters == nil {
		return nil, nil
	}
	assignment, found, err := d.clusters.Lookup(ctx, normalizedURL)
	if err != nil {
		return nil, fmt.Errorf("look up document content cluster: %w", err)
	}
	if _, err := d.clusters.Delete(ctx, normalizedURL); err != nil {
		return nil, fmt.Errorf("delete document content cluster: %w", err)
	}
	if !found {
		return nil, nil
	}
	cluster, found, err := d.clusters.Cluster(ctx, assignment.ClusterID)
	if err != nil {
		return nil, fmt.Errorf("read surviving document content cluster: %w", err)
	}
	if !found || d.directory == nil {
		return nil, nil
	}

	updates := make([]documentstore.Document, 0, len(cluster.MemberURLs))
	for _, memberURL := range cluster.MemberURLs {
		doc, found, err := d.directory.Document(ctx, memberURL)
		if err != nil {
			return nil, fmt.Errorf("read surviving clustered document: %w", err)
		}
		if !found || doc.ClusterID == cluster.ID &&
			doc.RepresentativeURL == cluster.RepresentativeURL {
			continue
		}
		doc.ClusterID = cluster.ID
		doc.RepresentativeURL = cluster.RepresentativeURL
		updates = append(updates, doc)
	}

	return updates, nil
}

func (d documentLineageEvictor) commitUpdates(
	ctx context.Context,
	updates map[string]documentstore.Document,
) error {
	if len(updates) == 0 {
		return nil
	}
	urls := make([]string, 0, len(updates))
	for url := range updates {
		urls = append(urls, url)
	}
	sort.Strings(urls)
	docs := make([]documentstore.Document, 0, len(urls))
	for _, url := range urls {
		docs = append(docs, updates[url])
	}

	if d.receiver != nil {
		receipt, err := d.receiver.Receive(ctx, docs)
		if err != nil {
			return fmt.Errorf("store document lineage updates: %w", err)
		}
		if receipt.Busy {
			return fmt.Errorf("store document lineage updates at capacity")
		}
		if len(receipt.CommittedDocuments) > 0 {
			docs = receipt.CommittedDocuments
		}
	}
	if d.index == nil {
		return nil
	}
	for _, doc := range docs {
		if err := d.index.Index(ctx, doc); err != nil {
			return fmt.Errorf("index document lineage update: %w", err)
		}
	}

	return nil
}
