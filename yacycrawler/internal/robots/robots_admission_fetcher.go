package robots

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/temoto/robotstxt"
	"golang.org/x/sync/singleflight"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
)

type RobotsAdmissionFetcher struct {
	inner     pagefetch.PageSource
	client    *http.Client
	userAgent string
	groups    *lru.Cache[string, *robotstxt.Group]
	fetches   singleflight.Group
}

func NewRobotsAdmissionFetcher(
	inner pagefetch.PageSource,
	client *http.Client,
	userAgent string,
	hostCacheSize int,
) (*RobotsAdmissionFetcher, error) {
	groups, err := lru.New[string, *robotstxt.Group](hostCacheSize)
	if err != nil {
		return nil, fmt.Errorf("robots host cache: %w", err)
	}
	return &RobotsAdmissionFetcher{
		inner:     inner,
		client:    client,
		userAgent: userAgent,
		groups:    groups,
	}, nil
}

func (f *RobotsAdmissionFetcher) Fetch(
	ctx context.Context,
	rawURL string,
) (pagefetch.FetchedPage, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("parse url: %w", err)
	}
	group := f.group(ctx, target)
	if !group.Test(target.Path) {
		return pagefetch.FetchedPage{}, fmt.Errorf("robots disallow: %w", pagefetch.ErrPageRejected)
	}
	page, err := f.inner.Fetch(ctx, rawURL)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("inner fetch: %w", err)
	}
	return page, nil
}

func (f *RobotsAdmissionFetcher) group(ctx context.Context, target *url.URL) *robotstxt.Group {
	if group, ok := f.groups.Get(target.Host); ok {
		return group
	}
	resolved, _, _ := f.fetches.Do(target.Host, func() (any, error) {
		group := f.fetchRobotsGroup(ctx, target)
		f.groups.Add(target.Host, group)
		return group, nil
	})
	return resolved.(*robotstxt.Group)
}

func (f *RobotsAdmissionFetcher) fetchRobotsGroup(
	ctx context.Context,
	target *url.URL,
) *robotstxt.Group {
	robotsURL := target.Scheme + "://" + target.Host + "/robots.txt"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return allowAll()
	}
	response, err := f.client.Do(request)
	if err != nil {
		slog.WarnContext(
			ctx,
			"robots fetch failed",
			slog.String("host", target.Host),
			slog.Any("error", err),
		)
		return allowAll()
	}
	defer func() {
		if cerr := response.Body.Close(); cerr != nil {
			slog.WarnContext(
				ctx,
				"robots body close failed",
				slog.String("host", target.Host),
				slog.Any("error", cerr),
			)
		}
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return allowAll()
	}
	data, err := robotstxt.FromStatusAndBytes(response.StatusCode, body)
	if err != nil {
		return allowAll()
	}
	return data.FindGroup(f.userAgent)
}

func allowAll() *robotstxt.Group {
	data, _ := robotstxt.FromBytes(nil)
	return data.FindGroup("*")
}
