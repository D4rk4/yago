package crawlurls

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

type URLDirectory interface {
	RowsByHash(context.Context, []yacymodel.Hash) ([]yacymodel.URIMetadataRow, error)
}

type RemoteCrawlURLs interface {
	URLsForRemoteCrawl(context.Context, int, time.Duration) ([]RemoteCrawlURL, error)
}

type RemoteCrawlURL struct {
	Link        string
	Referrer    string
	Description string
	PublishedAt time.Time
	GUID        yacymodel.Hash
}

type DisabledRemoteCrawlURLs struct{}

func (DisabledRemoteCrawlURLs) URLsForRemoteCrawl(
	context.Context,
	int,
	time.Duration,
) ([]RemoteCrawlURL, error) {
	return nil, nil
}

func Mount(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	urls URLDirectory,
	remote RemoteCrawlURLs,
) {
	httpguard.MountRaw(
		router,
		yacyproto.PathCrawlURLs,
		yacyproto.CrawlURLEndpointMethods,
		yacyproto.ParseCrawlURLRequest,
		newEndpoint(identity, urls, remote).Serve,
	)
}
