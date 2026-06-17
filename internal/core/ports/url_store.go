package ports

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type StoreURLsResult struct {
	Existing []yacymodel.Hash
	Rejected []yacymodel.Hash
}

type URLStore interface {
	StoreURLs(ctx context.Context, rows []yacymodel.URIMetadataRow) (StoreURLsResult, error)
	RowsByHash(ctx context.Context, hashes []yacymodel.Hash) ([]yacymodel.URIMetadataRow, error)
	URLCount(ctx context.Context) (int, error)
}
