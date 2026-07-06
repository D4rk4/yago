package crawltraversal

import (
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlfrontier"
)

type visitOutcome struct {
	entry      crawlfrontier.Entry
	candidates []discoveredLink
	counted    bool
	deferred   bool
	deferFor   time.Duration
	transient  bool
	err        error
}
