// Package rwi owns the transferRWI and search endpoints, RWI posting intake,
// posting storage with the referenced-URL set, and search. Its published port,
// PostingDirectory, is the only surface other modules import; it speaks the
// yacymodel vocabulary and lends cross-module purges a shared transaction, so the
// schema never leaks.
package rwi

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

type PurgeResult struct {
	PostingsDeleted   int
	ReferencesDeleted int
}

type PostingDirectory interface {
	RWICount(ctx context.Context) (int, error)
	ReferencedURLCount(ctx context.Context) (int, error)
	PurgeReferences(tx *boltvault.Txn, urls []yacymodel.Hash) (PurgeResult, error)
}

type PostingScanner interface {
	ScanWord(
		ctx context.Context,
		word yacymodel.Hash,
		visit func(yacymodel.RWIPosting) (bool, error),
	) error
}
