package robots

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/temoto/robotstxt"
	"golang.org/x/sync/singleflight"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

// ErrDisallowed marks a fetch refused because the host's robots.txt disallows the
// path. It wraps pagefetch.ErrPageRejected, so callers that only care that a page
// was rejected still match; callers that need the reason can test for this
// specifically (the pipeline counts it as a per-run robots denial).
var ErrDisallowed = errors.New("robots.txt disallowed")

const (
	msgRobotsRequestFailed  = "robots request build failed"
	msgRobotsFetchFailed    = "robots fetch failed"
	msgRobotsBodyCloseFail  = "robots body close failed"
	msgRobotsBodyReadFailed = "robots body read failed"
	msgRobotsParseFailed    = "robots parse failed"
)

type RobotsAdmissionFetcher struct {
	inner     pagefetch.PageSource
	client    *http.Client
	userAgent string
	groups    *lru.Cache[string, *robotstxt.Group]
	fetches   singleflight.Group
	observer  DenialObserver
}

func NewRobotsAdmissionFetcher(
	inner pagefetch.PageSource,
	client *http.Client,
	userAgent string,
	hostCacheSize int,
	opts ...Option,
) (*RobotsAdmissionFetcher, error) {
	groups, err := lru.New[string, *robotstxt.Group](hostCacheSize)
	if err != nil {
		return nil, fmt.Errorf("robots host cache: %w", err)
	}
	fetcher := &RobotsAdmissionFetcher{
		inner:     inner,
		client:    client,
		userAgent: userAgent,
		groups:    groups,
		observer:  noopDenialObserver{},
	}
	for _, opt := range opts {
		opt(fetcher)
	}

	return fetcher, nil
}

func (f *RobotsAdmissionFetcher) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	group := f.group(ctx, target)
	if !group.Test(target.Path) {
		f.observer.RobotsDenied()

		return pagefetch.FetchedPage{}, fmt.Errorf(
			"robots disallow: %w: %w", ErrDisallowed, pagefetch.ErrPageRejected,
		)
	}
	page, err := f.inner.Fetch(ctx, target)
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
		group, ok := f.fetchRobotsGroup(ctx, target)
		if ok {
			f.groups.Add(target.Host, group)
		}
		return group, nil
	})
	return resolved.(*robotstxt.Group)
}

func (f *RobotsAdmissionFetcher) fetchRobotsGroup(
	ctx context.Context,
	target *url.URL,
) (*robotstxt.Group, bool) {
	robotsURL := target.Scheme + "://" + target.Host + "/robots.txt"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgRobotsRequestFailed,
			slog.String("host", target.Host),
			slog.Any("error", err),
		)
		return allowAll(), false
	}
	response, err := f.client.Do(request)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgRobotsFetchFailed,
			slog.String("host", target.Host),
			slog.Any("error", err),
		)
		return allowAll(), false
	}
	defer func() {
		if cerr := response.Body.Close(); cerr != nil {
			slog.WarnContext(
				ctx,
				msgRobotsBodyCloseFail,
				slog.String("host", target.Host),
				slog.Any("error", cerr),
			)
		}
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgRobotsBodyReadFailed,
			slog.String("host", target.Host),
			slog.Any("error", err),
		)
		return allowAll(), false
	}
	data, err := robotstxt.FromStatusAndBytes(response.StatusCode, body)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgRobotsParseFailed,
			slog.String("host", target.Host),
			slog.Any("error", err),
		)
		return allowAll(), false
	}
	return data.FindGroup(f.userAgent), true
}

func allowAll() *robotstxt.Group {
	data, _ := robotstxt.FromBytes(nil)
	return data.FindGroup("*")
}
