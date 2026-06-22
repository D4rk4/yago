package eviction

import (
	"github.com/nikitakarpei/yacy-rwi-node/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/internal/urlmeta"
)

func New(
	vault *boltvault.Vault,
	postings rwi.PostingDirectory,
	urls urlmeta.URLEvictor,
	cfg Config,
) Sweeper {
	return Sweeper{
		vault:    vault,
		postings: postings,
		urls:     urls,
		target:   cfg.TargetFraction,
		batch:    cfg.BatchSize,
	}
}
