package crawledpage

import (
	"context"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
)

type noopCrawledPageEmitter struct{}

func NewNoopCrawledPageEmitter() CrawledPageEmitter {
	return noopCrawledPageEmitter{}
}

func (noopCrawledPageEmitter) Emit(context.Context, pageparse.ParsedPage, time.Time) error {
	return nil
}
