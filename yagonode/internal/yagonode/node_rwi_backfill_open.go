package yagonode

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/rwibackfill"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func openNodeRWIBackfill(vault *vault.Vault, storage nodeStorage) (*rwibackfill.Backfill, error) {
	resumableDocuments, ok := storage.documentDirectory.(documentstore.ResumableStoredDocumentPageScanner)
	if !ok {
		return nil, fmt.Errorf("resumable stored-document scan is unavailable")
	}
	backfill, err := rwibackfill.Open(
		vault,
		resumableDocuments,
		storage.urlReceiver,
		storage.postingReceiver,
	)
	if err != nil {
		return nil, fmt.Errorf("open rwi backfill: %w", err)
	}
	return backfill, nil
}
