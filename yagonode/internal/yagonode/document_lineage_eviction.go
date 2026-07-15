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
	Delete(context.Context, string) error
}

type documentBatchUpdateIndexer interface {
	IndexBatch(context.Context, []documentstore.Document) error
}

type documentLineageEvictor struct {
	directory       documentstore.DocumentDirectory
	receiver        documentstore.DocumentReceiver
	documents       documentstore.DocumentEvictor
	anchors         documentstore.InboundAnchorReceiver
	lineages        documentstore.DocumentLineageReserver
	reservedAnchors documentstore.ReservedOutboundAnchorReceiver
	clusters        contentClusterDocumentEvictor
	index           documentUpdateIndexer
}

func newDocumentLineageEvictor(storage nodeStorage) documentstore.DocumentEvictor {
	documents, _ := storage.documentDirectory.(documentstore.DocumentEvictor)
	if documents == nil {
		return nil
	}
	anchors, _ := storage.documentReceiver.(documentstore.InboundAnchorReceiver)
	lineages, _ := storage.documentReceiver.(documentstore.DocumentLineageReserver)
	reservedAnchors, _ := storage.documentReceiver.(documentstore.ReservedOutboundAnchorReceiver)

	return documentLineageEvictor{
		directory:       storage.documentDirectory,
		receiver:        storage.documentReceiver,
		documents:       documents,
		anchors:         anchors,
		lineages:        lineages,
		reservedAnchors: reservedAnchors,
		clusters:        storage.contentClusters,
		index:           storage.searchIndex,
	}
}

func (d documentLineageEvictor) Delete(
	ctx context.Context,
	normalizedURL string,
) (bool, error) {
	reservation, err := d.ReserveDocumentEvictions(ctx, []string{normalizedURL})
	if err != nil {
		return false, err
	}
	defer reservation.Release()

	removed, err := reservation.Delete(ctx, normalizedURL)
	if err != nil {
		return false, fmt.Errorf("delete reserved document lineage: %w", err)
	}

	return removed, nil
}

func (d documentLineageEvictor) deleteReservedDocumentLineage(
	ctx context.Context,
	reservation documentstore.DocumentLineageReservation,
	normalizedURL string,
) (bool, error) {
	clusterDeletion, err := d.beginContentClusterDeletion(ctx, normalizedURL)
	if err != nil {
		return false, err
	}
	defer clusterDeletion.release()
	sourceDocument, sourceFound, err := d.deletedDocumentLineage(ctx, normalizedURL)
	if err != nil {
		return false, err
	}
	clusterDeletion, err = d.projectContentClusterDeletion(
		ctx,
		clusterDeletion,
		normalizedURL,
		sourceDocument,
		sourceFound,
	)
	if err != nil {
		return false, err
	}
	if err := d.clearOutboundAnchorContributions(
		ctx,
		reservation,
		normalizedURL,
	); err != nil {
		return false, err
	}

	updates := make(map[string]documentstore.Document, len(clusterDeletion.updates))
	for _, doc := range clusterDeletion.updates {
		updates[doc.NormalizedURL] = doc
	}
	if err := d.commitUpdates(ctx, updates); err != nil {
		return false, err
	}
	removed, err := d.documents.Delete(ctx, normalizedURL)
	if err != nil {
		return false, fmt.Errorf("delete stored document: %w", err)
	}
	if d.index != nil {
		if err := d.index.Delete(ctx, normalizedURL); err != nil {
			return false, fmt.Errorf("delete indexed document: %w", err)
		}
	}
	if err := clusterDeletion.finalize(ctx); err != nil {
		return false, err
	}

	return removed, nil
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
	if batch, ok := d.index.(documentBatchUpdateIndexer); ok {
		if err := batch.IndexBatch(ctx, docs); err != nil {
			return fmt.Errorf("index document lineage update batch: %w", err)
		}

		return nil
	}
	for _, doc := range docs {
		if err := d.index.Index(ctx, doc); err != nil {
			return fmt.Errorf("index document lineage update: %w", err)
		}
	}

	return nil
}
