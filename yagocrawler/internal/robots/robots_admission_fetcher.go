package robots

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/temoto/robotstxt"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

// ErrDisallowed marks a fetch refused because the host's robots.txt disallows the
// path. It wraps pagefetch.ErrPageRejected, so callers that only care that a page
// was rejected still match; callers that need the reason can test for this
// specifically (the pipeline counts it as a per-run robots denial).
var ErrDisallowed = errors.New("robots.txt disallowed")

const (
	maximumRobotsBytes      = 500 * 1024
	msgRobotsRequestFailed  = "robots request build failed"
	msgRobotsFetchFailed    = "robots fetch failed"
	msgRobotsBodyCloseFail  = "robots body close failed"
	msgRobotsBodyReadFailed = "robots body read failed"
	msgRobotsParseFailed    = "robots parse failed"
	msgRobotsUnavailable    = "robots unavailable"
)

type RobotsAdmissionFetcher struct {
	inner     pagefetch.PageSource
	client    *http.Client
	userAgent string
	policies  *originPolicyCache
	observer  DenialObserver
}

func NewRobotsAdmissionFetcher(
	inner pagefetch.PageSource,
	client *http.Client,
	userAgent string,
	hostCacheSize int,
	opts ...Option,
) (*RobotsAdmissionFetcher, error) {
	policies, err := newOriginPolicyCache(hostCacheSize)
	if err != nil {
		return nil, fmt.Errorf("robots origin cache: %w", err)
	}
	fetcher := &RobotsAdmissionFetcher{
		inner:     inner,
		client:    client,
		userAgent: userAgent,
		policies:  policies,
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

func (f *RobotsAdmissionFetcher) fetchRobotsGroup(
	ctx context.Context,
	target *url.URL,
) originPolicyRefresh {
	robotsURL := target.Scheme + "://" + target.Host + "/robots.txt"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgRobotsRequestFailed,
			slog.String("host", target.Host),
			slog.Any("error", err),
		)
		return unreachableOriginPolicy()
	}
	response, err := f.client.Do(request)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgRobotsFetchFailed,
			slog.String("host", target.Host),
			slog.Any("error", err),
		)
		return unreachableOriginPolicy()
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
	if response.StatusCode >= http.StatusInternalServerError && response.StatusCode < 600 {
		slog.WarnContext(
			ctx,
			msgRobotsUnavailable,
			slog.String("host", target.Host),
			slog.Int("status", response.StatusCode),
		)
		return unreachableOriginPolicy()
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumRobotsBytes))
	if err != nil {
		slog.WarnContext(
			ctx,
			msgRobotsBodyReadFailed,
			slog.String("host", target.Host),
			slog.Any("error", err),
		)
		return unreachableOriginPolicy()
	}
	return originPolicyRefresh{
		group:    f.parseRobotsGroup(ctx, target.Host, response.StatusCode, body),
		lifetime: robotsPolicyFreshness,
	}
}

// parseRobotsGroup resolves a fetched robots.txt body into this crawler's group.
// A fetched body is a deterministic input, so the caller always caches the
// result — re-fetching an unparseable file would only repeat the failure and
// flood the log. A strict parser rejects some common real-world files (a
// directive before the first User-agent), so a parse failure is retried once on
// a sanitized body to still honor the host's rules; only when that also fails
// does the crawler fall back to allow-all, logged once per cache lifetime.
func (f *RobotsAdmissionFetcher) parseRobotsGroup(
	ctx context.Context,
	host string,
	statusCode int,
	body []byte,
) *robotstxt.Group {
	data, err := robotstxt.FromStatusAndBytes(statusCode, body)
	if err != nil {
		data, err = robotstxt.FromStatusAndBytes(statusCode, sanitizeRobots(body))
	}
	if err != nil {
		slog.WarnContext(
			ctx,
			msgRobotsParseFailed,
			slog.String("host", host),
			slog.Any("error", err),
		)

		return allowAll()
	}

	return data.FindGroup(f.userAgent)
}

func allowAll() *robotstxt.Group {
	data, _ := robotstxt.FromBytes(nil)
	return data.FindGroup("*")
}

func disallowAll() *robotstxt.Group {
	data, _ := robotstxt.FromBytes([]byte("User-agent: *\nDisallow: /\n"))
	return data.FindGroup("*")
}
