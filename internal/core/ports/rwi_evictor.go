package ports

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type RWIEvictor interface {
	UsedBytes(ctx context.Context) (int64, error)
	SelectEvictionCandidates(ctx context.Context, limit int) ([]yacymodel.Hash, error)
	DeleteURLs(ctx context.Context, urls []yacymodel.Hash) (EvictionResult, error)
}

type EvictionResult struct {
	URLsDeleted     int
	PostingsDeleted int
}
