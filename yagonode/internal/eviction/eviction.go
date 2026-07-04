// Package eviction frees storage when the vault nears its quota. It owns no
// buckets: it reads usage from the storage kernel, names the stalest URLs, looks
// up the words referencing each, and drops their postings and metadata within one
// capacity-exempt transaction, so every collection clears atomically without
// sharing a schema.
package eviction

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/urlmetastaleness"
	"github.com/D4rk4/yago/yagonode/internal/urlreferences"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type Config struct {
	TargetFraction float64
	BatchSize      int
}

type Result struct {
	URLsDeleted     int
	PostingsDeleted int
}

type Sweeper interface {
	Sweep(ctx context.Context) (Result, error)
}

//nolint:revive // each argument is a distinct collection the sweep prunes; bundling them would invent a hollow type
func NewSweeper(
	vault *vault.Vault,
	postings rwi.PostingPurger,
	references urlreferences.ReferenceQuery,
	urls urlmeta.URLEvictor,
	stale urlmetastaleness.StaleURLSource,
	cfg Config,
) Sweeper {
	return quotaSweeper{
		vault:      vault,
		postings:   postings,
		references: references,
		urls:       urls,
		stale:      stale,
		target:     cfg.TargetFraction,
		batch:      cfg.BatchSize,
	}
}
