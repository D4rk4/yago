package searchindex

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const bleveRebuildBatchDocuments = 16

type bleveRebuildBatchObserver interface {
	BleveRebuildBatchIndexed(int)
}

var indexBleveRebuildBatch = func(
	index *BleveDiskIndex,
	ctx context.Context,
	documents []documentstore.Document,
) error {
	return index.IndexBatch(ctx, documents)
}

func (b *BleveDiskIndex) rebuild(
	ctx context.Context,
	stored documentstore.StoredDocuments,
	admissions ...BleveRebuildGrowthAdmission,
) error {
	admission := firstBleveRebuildGrowthAdmission(admissions)
	documents := make([]documentstore.Document, 0, bleveRebuildBatchDocuments)
	flush := func() error {
		if len(documents) == 0 {
			return nil
		}
		if admission != nil {
			if err := admission.CheckGrowth(); err != nil {
				return fmt.Errorf("bleve rebuild growth admission: %w", err)
			}
		}
		if err := indexBleveRebuildBatch(b, ctx, documents); err != nil {
			return err
		}
		if observer, ok := admission.(bleveRebuildBatchObserver); ok {
			observer.BleveRebuildBatchIndexed(len(documents))
		}
		clear(documents)
		documents = documents[:0]

		return nil
	}
	if err := stored.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		documents = append(documents, doc)
		if len(documents) < bleveRebuildBatchDocuments {
			return true, nil
		}

		return true, flush()
	}); err != nil {
		return fmt.Errorf("rebuild bleve disk index: %w", err)
	}
	if err := flush(); err != nil {
		return fmt.Errorf("rebuild bleve disk index: %w", err)
	}

	return nil
}
