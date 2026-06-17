package ports

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type AppendRWIResult struct {
	Rejected    []yacymodel.Hash
	UnknownURLs []yacymodel.Hash
}

type RWIStore interface {
	AppendRWI(ctx context.Context, entries []yacymodel.RWIEntry) (AppendRWIResult, error)
	PostingsForWords(
		ctx context.Context,
		wordHashes []yacymodel.Hash,
		limitPerWord int,
	) (map[yacymodel.Hash][]yacymodel.RWIEntry, error)
	RWICount(ctx context.Context) (int, error)
	ReferencedURLCount(ctx context.Context) (int, error)
}
