package crawlurls

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
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
	req yagoproto.CrawlURLRequest,
) (httpguard.RawResponse, error) {
	feed := e.rejectedFeed()
	if !e.identity.Authenticates(req.NetworkName, req.Key, req.Iam, req.MagicMD5) {
		return feed.response(), nil
	}

	switch req.Call {
	case yagoproto.CrawlURLCallRemoteCrawl:
		return e.serveRemoteCrawl(ctx, req, feed)
	case yagoproto.CrawlURLCallURLHashList:
		return e.serveURLHashList(ctx, req, feed)
	default:
		return feed.response(), nil
	}
}

func (e endpoint) serveRemoteCrawl(
	ctx context.Context,
	req yagoproto.CrawlURLRequest,
	feed crawlURLFeed,
) (httpguard.RawResponse, error) {
	peer, valid := remoteCrawlPeer(req.Iam)
	if !valid {
		return feed.response(), nil
	}
	selection := e.selectRemoteCrawlURLs(
		ctx,
		peer,
		remoteURLCount(req.Count),
		remoteURLTimeout(req.Time),
	)
	if selection.failure != nil {
		return httpguard.RawResponse{}, selection.failure
	}
	if !selection.accepted {
		return feed.response(), nil
	}

	feed.Response = yagoproto.CrawlURLResponseOK
	feed.Items = remoteCrawlItems(selection.items)

	return feed.response(), nil
}

func remoteCrawlPeer(raw string) (yagomodel.Hash, bool) {
	peer, err := yagomodel.ParseHash(raw)

	return peer, err == nil
}

type remoteCrawlSelection struct {
	items    []RemoteCrawlURL
	accepted bool
	failure  error
}

func (e endpoint) selectRemoteCrawlURLs(
	ctx context.Context,
	peer yagomodel.Hash,
	count int,
	wait time.Duration,
) remoteCrawlSelection {
	items, err := e.remote.URLsForRemoteCrawl(ctx, peer, count, wait)
	if errors.Is(err, ErrRemoteCrawlRejected) {
		return remoteCrawlSelection{}
	}
	if err != nil {
		return remoteCrawlSelection{failure: fmt.Errorf("remote crawl urls: %w", err)}
	}

	return remoteCrawlSelection{items: items, accepted: true}
}

func (e endpoint) serveURLHashList(
	ctx context.Context,
	req yagoproto.CrawlURLRequest,
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

	feed.Response = yagoproto.CrawlURLResponseOK
	feed.Items = items

	return feed.response(), nil
}

func (e endpoint) referrerURLs(
	ctx context.Context,
	rows []yagomodel.URIMetadataRow,
) (map[yagomodel.Hash]string, error) {
	hashes := referrerHashes(rows)
	if len(hashes) == 0 {
		return nil, nil
	}

	rows, err := e.urls.RowsByHash(ctx, hashes)
	if err != nil {
		return nil, fmt.Errorf("referrer url metadata rows: %w", err)
	}

	referrers := make(map[yagomodel.Hash]string, len(rows))
	for _, row := range rows {
		hash, err := row.URLHash()
		if err != nil {
			return nil, fmt.Errorf("referrer url metadata hash: %w", err)
		}
		link, err := decodedURLProperty(ctx, row, yagomodel.URLMetaURL)
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
		Response: yagoproto.CrawlURLResponseRejected,
	}
}

func remoteURLCount(count yagomodel.Optional[int]) int {
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

func remoteURLTimeout(timeout yagomodel.Optional[int]) time.Duration {
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

func referrerHashes(rows []yagomodel.URIMetadataRow) []yagomodel.Hash {
	var hashes []yagomodel.Hash
	for _, row := range rows {
		if raw := row.Properties[yagomodel.URLMetaReferrer]; raw != "" {
			hashes = append(hashes, yagomodel.Hash(raw))
		}
	}

	return hashes
}
