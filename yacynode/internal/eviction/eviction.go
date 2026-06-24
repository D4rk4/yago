// Package eviction frees storage when the vault nears its quota. It owns no
// buckets: it reads usage from the storage kernel, asks urlmeta for the stalest
// URLs, and purges them from rwi and urlmeta within one capacity-exempt
// transaction, so both collections drop atomically without sharing a schema.
package eviction

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
)

type Config struct {
	TargetFraction float64
	BatchSize      int
}

type Result struct {
	URLsDeleted     int
	PostingsDeleted int
}

type StaleURLSource interface {
	StalestURLs(ctx context.Context, limit int) ([]yacymodel.Hash, error)
}

type Sweeper interface {
	Sweep(ctx context.Context) (Result, error)
}

func NewSweeper(
	vault *boltvault.Vault,
	postings rwi.PostingDirectory,
	urls urlmeta.URLEvictor,
	stale StaleURLSource,
	cfg Config,
) Sweeper {
	return quotaSweeper{
		vault:    vault,
		postings: postings,
		urls:     urls,
		stale:    stale,
		target:   cfg.TargetFraction,
		batch:    cfg.BatchSize,
	}
}
