package crawlurls

import (
	"context"
	"errors"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

var ErrRemoteCrawlRejected = errors.New("remote crawl request rejected")

type URLDirectory interface {
	RowsByHash(context.Context, []yagomodel.Hash) ([]yagomodel.URIMetadataRow, error)
}

type RemoteCrawlURLs interface {
	URLsForRemoteCrawl(
		context.Context,
		yagomodel.Hash,
		int,
		time.Duration,
	) ([]RemoteCrawlURL, error)
}

type RemoteCrawlURL struct {
	Link        string
	Referrer    string
	Description string
	PublishedAt time.Time
	GUID        yagomodel.Hash
}

type DisabledRemoteCrawlURLs struct{}

func (DisabledRemoteCrawlURLs) URLsForRemoteCrawl(
	context.Context,
	yagomodel.Hash,
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
		yagoproto.PathCrawlURLs,
		yagoproto.CrawlURLEndpointMethods,
		yagoproto.ParseCrawlURLRequest,
		newEndpoint(identity, urls, remote).Serve,
	)
}
