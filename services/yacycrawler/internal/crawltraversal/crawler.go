package crawltraversal

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

type Crawler struct {
	config   Config
	fetch    crawlcapability.PageRetrieval
	extract  crawlcapability.DocumentExtraction
	recrawl  crawlcapability.RecrawlDecision
	outputs  []crawlcapability.PagePublication
	observer crawlcapability.RunProgress
	clock    crawlcapability.Clock
}

//nolint:revive // argument-limit: the crawler's collaborators are all distinct ports.
func NewCrawler(
	config Config,
	fetch crawlcapability.PageRetrieval,
	extract crawlcapability.DocumentExtraction,
	recrawl crawlcapability.RecrawlDecision,
	outputs []crawlcapability.PagePublication,
	observer crawlcapability.RunProgress,
	clock crawlcapability.Clock,
) *Crawler {
	return &Crawler{
		config:   config,
		fetch:    fetch,
		extract:  extract,
		recrawl:  recrawl,
		outputs:  outputs,
		observer: observer,
		clock:    clock,
	}
}

func (r *Crawler) Traverse(ctx context.Context, delivery crawlcapability.DeliveredOrder) error {
	return r.newCrawl(delivery).run(ctx)
}
