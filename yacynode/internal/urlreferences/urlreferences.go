// Package urlreferences knows, for every url, the words that reference it, so
// eviction can drop a url's postings without scanning the word index. It is a
// read-only projection of rwi: it mutates its own buckets only in reaction to
// rwi posting arrivals and departures inside the caller's transaction, so its
// knowledge never drifts from the postings it mirrors.
package urlreferences

import (
	"context"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type ReferenceQuery interface {
	WordsReferencing(tx *vault.Txn, url yacymodel.Hash) ([]yacymodel.Hash, error)
	ReferencedURLs(ctx context.Context, urls []yacymodel.Hash) ([]yacymodel.Hash, error)
	ReferencedURLCount(ctx context.Context) (int, error)
}

type ReferenceProjection interface {
	ReferenceQuery
	rwi.PostingObserver
}

func Open(vault *vault.Vault) (ReferenceProjection, error) {
	return openURLReferences(vault)
}
