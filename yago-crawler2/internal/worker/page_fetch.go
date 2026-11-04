package worker

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/httpfetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	messagePageFetchTiming        = "crawler page fetch timing"
	slowCrawlerOperationThreshold = time.Second
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
	startedAt := time.Now()
	timing := pageFetchTiming{}
	parsed, err := url.Parse(pageURL)
	if err != nil || parsed.Hostname() == "" {
		err = errors.New("crawler URL has no host")
		logPageFetchTiming(ctx, pageURL, timing, httpfetch.PageResponse{}, startedAt, err)
		return httpfetch.PageResponse{}, err
	}
	hostWaitStartedAt := time.Now()
	releaseHost, err := worker.hosts.Acquire(ctx, parsed.Hostname())
	timing.hostAdmissionWait = time.Since(hostWaitStartedAt)
	if err != nil {
		logPageFetchTiming(ctx, pageURL, timing, httpfetch.PageResponse{}, startedAt, err)
		return httpfetch.PageResponse{}, err
	}
	defer releaseHost()
	worker.active.Add(1)
	defer worker.active.Add(^uint32(0))
	httpStartedAt := time.Now()
	response, err := worker.fetcher.Fetch(ctx, pageURL)
	timing.httpRequestElapsed = time.Since(httpStartedAt)
	logPageFetchTiming(ctx, pageURL, timing, response, startedAt, err)
	return response, err
}

func logPageFetchTiming(
	ctx context.Context,
	pageURL string,
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
		slog.String("url", pageURL),
		slog.String("stage", "http_fetch"),
		slog.Duration("hostAdmissionWait", timing.hostAdmissionWait),
		slog.Duration("httpRequestElapsed", timing.httpRequestElapsed),
		slog.Duration("totalElapsed", timing.totalElapsed),
		slog.Bool("failed", fetchError != nil),
	}
	if response.StatusCode != 0 {
		attributes = append(attributes, slog.Int("status", response.StatusCode))
	}
	slog.InfoContext(ctx, messagePageFetchTiming, attributes...)
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
