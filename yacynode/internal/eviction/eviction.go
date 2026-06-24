// Package eviction frees storage when the vault nears its quota. It owns no
// buckets: it reads usage from the storage kernel, names the stalest URLs, looks
// up the words referencing each, and drops their postings and metadata within one
// capacity-exempt transaction, so every collection clears atomically without
// sharing a schema.
package eviction

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmetastaleness"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlreferences"
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
	vault *boltvault.Vault,
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
