package ports

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type RWIStore interface {
	AppendRWI(ctx context.Context, entries []yacymodel.RWIEntry) ([]yacymodel.Hash, error)
	PostingsForWords(
		ctx context.Context,
		wordHashes []yacymodel.Hash,
		limitPerWord int,
	) (map[yacymodel.Hash][]yacymodel.RWIEntry, error)
	RWICount(ctx context.Context) (int, error)
	ReferencedURLCount(ctx context.Context) (int, error)
}
