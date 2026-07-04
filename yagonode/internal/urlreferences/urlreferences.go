// Package urlreferences knows, for every url, the words that reference it, so
// eviction can drop a url's postings without scanning the word index. It is a
// read-only projection of rwi: it mutates its own buckets only in reaction to
// rwi posting arrivals and departures inside the caller's transaction, so its
// knowledge never drifts from the postings it mirrors.
package urlreferences

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type ReferenceQuery interface {
	WordsReferencing(tx *vault.Txn, url yagomodel.Hash) ([]yagomodel.Hash, error)
	ReferencedURLs(ctx context.Context, urls []yagomodel.Hash) ([]yagomodel.Hash, error)
	ReferencedURLCount(ctx context.Context) (int, error)
}

type ReferenceProjection interface {
	ReferenceQuery
	rwi.PostingObserver
}

func Open(vault *vault.Vault) (ReferenceProjection, error) {
	return openURLReferences(vault)
}
