package crawlurls

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

const (
	crawlURLContentType = "application/xml; charset=UTF-8"
	remoteDefaultCount  = 10
	remoteMaxCount      = 100
	remoteDefaultTime   = 10000
	remoteMinTime       = 1000
	remoteMaxTime       = 20000
)

type endpoint struct {
	identity nodeidentity.Identity
	urls     URLDirectory
	remote   RemoteCrawlURLs
	now      func() time.Time
}

func newEndpoint(
	identity nodeidentity.Identity,
	urls URLDirectory,
	remote RemoteCrawlURLs,
) endpoint {
	if remote == nil {
		remote = DisabledRemoteCrawlURLs{}
	}

	return endpoint{identity: identity, urls: urls, remote: remote, now: time.Now}
}

func (e endpoint) Serve(
	ctx context.Context,
	req yacyproto.CrawlURLRequest,
) (httpguard.RawResponse, error) {
	feed := e.rejectedFeed()
	if !e.identity.NetworkMatches(req.NetworkName) {
		return feed.response(), nil
	}

	switch req.Call {
	case yacyproto.CrawlURLCallRemoteCrawl:
		return e.serveRemoteCrawl(ctx, req, feed)
	case yacyproto.CrawlURLCallURLHashList:
		return e.serveURLHashList(ctx, req, feed)
	default:
		return feed.response(), nil
	}
}

func (e endpoint) serveRemoteCrawl(
	ctx context.Context,
	req yacyproto.CrawlURLRequest,
	feed crawlURLFeed,
) (httpguard.RawResponse, error) {
	items, err := e.remote.URLsForRemoteCrawl(
		ctx,
		remoteURLCount(req.Count),
		remoteURLTimeout(req.Time),
	)
	if err != nil {
		return httpguard.RawResponse{}, fmt.Errorf("remote crawl urls: %w", err)
	}

	feed.Response = yacyproto.CrawlURLResponseOK
	feed.Items = remoteCrawlItems(items)

	return feed.response(), nil
}

func (e endpoint) serveURLHashList(
	ctx context.Context,
	req yacyproto.CrawlURLRequest,
	feed crawlURLFeed,
) (httpguard.RawResponse, error) {
	hashes, ok := req.HashList()
	if !ok {
		return feed.response(), nil
	}

	rows, err := e.urls.RowsByHash(ctx, hashes)
	if err != nil {
		return httpguard.RawResponse{}, fmt.Errorf("url metadata rows: %w", err)
	}

	referrers, err := e.referrerURLs(ctx, rows)
	if err != nil {
		return httpguard.RawResponse{}, err
	}

	items, err := metadataItems(ctx, rows, referrers)
	if err != nil {
		return httpguard.RawResponse{}, err
	}

	feed.Response = yacyproto.CrawlURLResponseOK
	feed.Items = items

	return feed.response(), nil
}

func (e endpoint) referrerURLs(
	ctx context.Context,
	rows []yacymodel.URIMetadataRow,
) (map[yacymodel.Hash]string, error) {
	hashes := referrerHashes(rows)
	if len(hashes) == 0 {
		return nil, nil
	}

	rows, err := e.urls.RowsByHash(ctx, hashes)
	if err != nil {
		return nil, fmt.Errorf("referrer url metadata rows: %w", err)
	}

	referrers := make(map[yacymodel.Hash]string, len(rows))
	for _, row := range rows {
		hash, err := row.URLHash()
		if err != nil {
			return nil, fmt.Errorf("referrer url metadata hash: %w", err)
		}
		link, err := decodedURLProperty(ctx, row, yacymodel.URLMetaURL)
		if err != nil {
			return nil, err
		}
		referrers[hash.Hash()] = link
	}

	return referrers, nil
}

func (e endpoint) rejectedFeed() crawlURLFeed {
	now := e.now().UTC()

	return crawlURLFeed{
		Version:  e.identity.Version,
		Iam:      e.identity.Hash.String(),
		Uptime:   e.identity.Uptime(now),
		MyTime:   formatYaCyShortSecond(now),
		Response: yacyproto.CrawlURLResponseRejected,
	}
}

func remoteURLCount(count yacymodel.Optional[int]) int {
	n := remoteDefaultCount
	if requested, ok := count.Get(); ok {
		n = requested
	}
	if n > remoteMaxCount {
		return remoteMaxCount
	}
	if n < 0 {
		return 0
	}

	return n
}

func remoteURLTimeout(timeout yacymodel.Optional[int]) time.Duration {
	milliseconds := remoteDefaultTime
	if requested, ok := timeout.Get(); ok {
		milliseconds = requested
	}
	if milliseconds > remoteMaxTime {
		milliseconds = remoteMaxTime
	}
	if milliseconds < remoteMinTime {
		milliseconds = remoteMinTime
	}

	return time.Duration(milliseconds) * time.Millisecond
}

func referrerHashes(rows []yacymodel.URIMetadataRow) []yacymodel.Hash {
	var hashes []yacymodel.Hash
	for _, row := range rows {
		if raw := row.Properties[yacymodel.URLMetaReferrer]; raw != "" {
			hashes = append(hashes, yacymodel.Hash(raw))
		}
	}

	return hashes
}
