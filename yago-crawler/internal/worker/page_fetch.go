package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/httpfetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	messagePageFetchTiming        = "crawler page fetch timing"
	slowCrawlerOperationThreshold = time.Second
	robotsFetchTimeout            = 2 * time.Second
	messageRuntimePolicyActivated = "crawler runtime policy activated"
)

type pageFetchTiming struct {
	hostAdmissionWait  time.Duration
	httpRequestElapsed time.Duration
	totalElapsed       time.Duration
}

func (worker *Worker) fetchPage(
	ctx context.Context,
	pageURL string,
) (httpfetch.PageResponse, error) {
	return worker.fetchPageWithRequestTimeout(ctx, pageURL, 0, crawlerPageRequest)
}

func (worker *Worker) fetchRobotsPage(
	ctx context.Context,
	pageURL string,
) (httpfetch.PageResponse, error) {
	return worker.fetchPageWithRequestTimeout(ctx, pageURL, robotsFetchTimeout, crawlerRobotsRequest)
}

func (worker *Worker) fetchPageWithRequestTimeout(
	ctx context.Context,
	pageURL string,
	requestTimeout time.Duration,
	requestKind crawlerRequestKind,
) (httpfetch.PageResponse, error) {
	startedAt := time.Now()
	timing := pageFetchTiming{}
	parsed, err := url.Parse(pageURL)
	if err != nil || parsed.Hostname() == "" {
		err = errors.New("crawler URL has no host")
		logPageFetchTiming(ctx, "", timing, httpfetch.PageResponse{}, startedAt, err)
		return httpfetch.PageResponse{}, err
	}
	hostWaitStartedAt := time.Now()
	releaseHost, err := worker.hosts.Acquire(ctx, parsed.Hostname())
	timing.hostAdmissionWait = time.Since(hostWaitStartedAt)
	worker.activity.recordHostAdmission(requestKind, parsed.Hostname(), timing.hostAdmissionWait, err == nil)
	if err != nil {
		logPageFetchTiming(ctx, parsed.Hostname(), timing, httpfetch.PageResponse{}, startedAt, err)
		return httpfetch.PageResponse{}, err
	}
	defer releaseHost()
	worker.active.Add(1)
	defer worker.active.Add(^uint32(0))
	requestContext := ctx
	stopRequest := func() {}
	if requestTimeout > 0 {
		requestContext, stopRequest = context.WithTimeout(ctx, requestTimeout)
	}
	defer stopRequest()
	httpStartedAt := time.Now()
	response, err := worker.fetcher.Fetch(requestContext, pageURL)
	timing.httpRequestElapsed = time.Since(httpStartedAt)
	worker.activity.recordHTTPRequestCompleted(
		requestKind,
		parsed.Hostname(),
		timing.httpRequestElapsed,
		response.StatusCode,
		err != nil,
	)
	logPageFetchTiming(ctx, parsed.Hostname(), timing, response, startedAt, err)
	return response, err
}

func logPageFetchTiming(
	ctx context.Context,
	hostname string,
	timing pageFetchTiming,
	response httpfetch.PageResponse,
	startedAt time.Time,
	fetchError error,
) {
	timing.totalElapsed = time.Since(startedAt)
	if timing.totalElapsed < slowCrawlerOperationThreshold && fetchError == nil {
		return
	}
	attributes := []any{
		slog.String("host", hostname),
		slog.String("stage", "http_fetch"),
		slog.Duration("hostAdmissionWait", timing.hostAdmissionWait),
		slog.Duration("httpRequestElapsed", timing.httpRequestElapsed),
		slog.Duration("totalElapsed", timing.totalElapsed),
		slog.Bool("failed", fetchError != nil),
	}
	if response.StatusCode != 0 {
		attributes = append(attributes, slog.Int("status", response.StatusCode))
	}
	if fetchError != nil {
		attributes = append(attributes,
			slog.String("errorType", crawlerFetchErrorType(fetchError)),
			slog.Bool("timeout", errors.Is(fetchError, context.DeadlineExceeded)))
	}
	slog.InfoContext(ctx, messagePageFetchTiming, attributes...)
}

func crawlerFetchErrorType(fetchError error) string {
	if fetchError == nil {
		return ""
	}
	return fmt.Sprintf("%T", fetchError)
}

func hostnameFromPageURL(pageURL string) string {
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func logRuntimePolicy(ctx context.Context, policy yagocrawlcontract.CrawlerRuntimePolicy) {
	slog.InfoContext(
		ctx,
		messageRuntimePolicyActivated,
		slog.Duration("requestTimeout", policy.RequestTimeout),
		slog.Duration("connectTimeout", policy.ConnectTimeout),
		slog.Duration("headerTimeout", policy.HeaderTimeout),
		slog.Duration("crawlDelay", policy.CrawlDelay),
		slog.Int("maximumHostConcurrency", policy.MaximumHostConcurrency),
		slog.Int("maximumDepth", policy.MaximumDepth),
	)
}
