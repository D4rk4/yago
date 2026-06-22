package eviction

import (
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
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
