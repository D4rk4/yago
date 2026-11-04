package worker

import (
	"context"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler2/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const (
	maximumRetryAttempts      = 3
	leaseRecoveryInterval     = 5 * time.Second
	messagePageAttemptExpired = "crawler page attempt reached its deadline"
	messageRobotsDenied       = "crawler page blocked by robots policy"
	messagePageFetched        = "crawler page fetched"
	messagePageFailed         = "crawler page failed"
	messageIngestFailed       = "crawler document ingest delayed"
)

type sitemapLocation struct {
	URL string `xml:"loc"`
}

type sitemapDocument struct {
	Locations []sitemapLocation `xml:"url>loc"`
	Nested    []sitemapLocation `xml:"sitemap>loc"`
}

func (worker *Worker) receiveOrder(
	ctx context.Context,
	message *crawlrpc.CrawlOrderMessage,
) error {
	startedAt := time.Now()
	defer worker.releaseLease(message.GetLeaseId())
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(message.GetOrderJson())
	if err != nil || len(order.Provenance) == 0 {
		if err == nil {
			err = errors.New("crawl order has no run identity")
		}
		return errors.Join(err, worker.exchange.Requeue(ctx, message.GetLeaseId()))
	}
	executionStartedAt := time.Now()
	execution, err := worker.executeOrder(ctx, order, message.GetLeaseId())
	executionElapsed := time.Since(executionStartedAt)
	if err != nil {
		if ctx.Err() != nil {
			return errors.Join(ctx.Err(), worker.exchange.Requeue(ctx, message.GetLeaseId()))
		}
		slog.WarnContext(ctx, messageOrderFailed, slog.Any("error", err))
		return errors.Join(err, worker.exchange.Requeue(ctx, message.GetLeaseId()))
	}
	terminalAckStartedAt := time.Now()
	if err := worker.exchange.AcknowledgeTerminal(
		ctx,
		message.GetLeaseId(),
		message.GetOrderJson(),
		execution.tally,
		crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
	); err != nil {
		return err
	}
	terminalAckElapsed := time.Since(terminalAckStartedAt)
	slog.InfoContext(
		ctx,
		messageOrderDone,
		slog.String("runId", hex.EncodeToString(order.Provenance)),
		slog.Int("receivedRequests", execution.seeds.receivedRequests),
		slog.Int("admittedPages", execution.seeds.admittedPages),
		slog.Int("filteredRequests", execution.seeds.filteredRequests),
		slog.Int("hostLimitedRequests", execution.seeds.hostLimitedRequests),
		slog.Int("duplicateRequests", execution.seeds.duplicateRequests),
		slog.Uint64("fetched", execution.tally.Fetched),
		slog.Uint64("indexed", execution.tally.Indexed),
		slog.Uint64("failed", execution.tally.Failed),
		slog.Duration("executionElapsed", executionElapsed),
		slog.Duration("terminalAckElapsed", terminalAckElapsed),
		slog.Duration("elapsed", time.Since(startedAt)),
	)
	return nil
}

func (worker *Worker) executeOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	leaseID string,
) (orderExecutionSummary, error) {
	execution := orderExecutionSummary{
		seeds: seedAdmission{receivedRequests: len(order.Requests)},
	}
	if _, err := worker.frontier.RecoverExpired(ctx, time.Now()); err != nil {
		return execution, err
	}
	rules, err := compileRunRules(order, worker.policy)
	if err != nil {
		execution.tally.Failed = 1
		return execution, nil
	}
	runID := hex.EncodeToString(order.Provenance)
	worker.waitingPageReports.runStarted(runID)
	defer worker.waitingPageReports.runFinished(runID)
	if err := worker.frontier.RebindPendingIngestLease(ctx, runID, leaseID); err != nil {
		return execution, err
	}
	execution.seeds, err = admitOrderSeeds(ctx, worker.frontier, runID, order.Requests, rules)
	if err != nil {
		return execution, err
	}
	nextLeaseRecovery := time.Now().Add(leaseRecoveryInterval)
	pageResults := make(chan pageExecutionOutcome, yagocrawlcontract.MaximumFetchWorkerConcurrency)
	inFlight := 0
	for {
		if err := ctx.Err(); err != nil {
			return execution, err
		}
		if !time.Now().Before(nextLeaseRecovery) {
			if _, err := worker.frontier.RecoverExpired(ctx, time.Now()); err != nil {
				return execution, err
			}
			nextLeaseRecovery = time.Now().Add(leaseRecoveryInterval)
		}
		if release, admitted := worker.fetchAdmission.TryAcquire(); admitted {
			page, found, err := worker.frontier.Claim(ctx, runID, time.Now())
			if err != nil {
				release()
				return execution, err
			}
			if found {
				inFlight++
				go func(claimed frontier.Page) {
					err := worker.executeClaimedPage(ctx, order, rules, runID, leaseID, claimed)
					pageResults <- pageExecutionOutcome{err: err}
					release()
				}(page)
				continue
			}
			release()
		}
		depth, err := worker.frontier.DepthForRun(ctx, runID)
		if err != nil {
			return execution, err
		}
		if depth.Pending() == 0 && inFlight == 0 {
			pending, err := worker.frontier.PendingIngestForRun(ctx, runID)
			if err != nil {
				return execution, err
			}
			if pending == 0 {
				execution.tally = runTally(depth)
				return execution, nil
			}
		}
		if inFlight > 0 {
			select {
			case outcome := <-pageResults:
				inFlight--
				if outcome.err != nil {
					return execution, outcome.err
				}
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return execution, ctx.Err()
			}
			continue
		}
		if !waitContext(ctx, 100*time.Millisecond) {
			return execution, ctx.Err()
		}
	}
}

func (worker *Worker) processPage(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	rules runRules,
	runID string,
	page frontier.Page,
) (pageProcessingResult, error) {
	defer worker.clearPageActivity(page.ID)
	worker.setPageActivity(runID, page.ID, page.URL, "robots")
	logPageStageStarted(ctx, page.URL, "robots")
	robotsStartedAt := time.Now()
	allowed, err := worker.robots.Allows(ctx, page.URL, order.Profile.IgnoreRobots)
	logPageProcessingTiming(ctx, page.URL, "robots", time.Since(robotsStartedAt), err != nil)
	if err != nil {
		if ctx.Err() != nil {
			return worker.deferredPageResult(page, "robots"), nil
		}
		return pageProcessingResult{state: frontier.StateFailed}, err
	}
	result := pageProcessingResult{state: frontier.StateFetched, stage: "robots"}
	if !allowed {
		slog.DebugContext(ctx, messageRobotsDenied,
			slog.String("url", page.URL), slog.String("host", page.Host))
		return pageProcessingResult{state: frontier.StateBlocked}, nil
	}
	worker.setPageActivity(runID, page.ID, page.URL, "http_fetch")
	logPageStageStarted(ctx, page.URL, "http_fetch")
	response, err := worker.fetchPage(ctx, page.URL)
	if err != nil {
		return pageProcessingResult{
			state:   worker.retryOrFail(page, err, 0),
			retryAt: retryDeadline(page, "", time.Now()),
			stage:   "http_fetch",
		}, nil
	}
	result.stage = "http_fetch"
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return pageProcessingResult{
			state:   worker.retryOrFail(page, nil, response.StatusCode),
			retryAt: retryDeadline(page, response.RetryAfter, time.Now()),
		}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		slog.WarnContext(ctx, messagePageFailed,
			slog.String("url", page.URL), slog.Int("status", response.StatusCode))
		return pageProcessingResult{state: frontier.StateFailed}, nil
	}
	mediaType, _, _ := mime.ParseMediaType(response.ContentType)
	if strings.EqualFold(mediaType, "application/xml") || strings.EqualFold(mediaType, "text/xml") ||
		strings.Contains(strings.ToLower(response.ContentType), "sitemap") {
		if err := worker.enqueueSitemap(ctx, response.Body, rules, runID, page.Depth+1); err != nil {
			if ctx.Err() != nil {
				return worker.deferredPageResult(page, "sitemap"), nil
			}
			return pageProcessingResult{state: frontier.StateFailed}, err
		}
		return result, nil
	}
	if !strings.EqualFold(mediaType, "text/html") &&
		!strings.EqualFold(mediaType, "application/xhtml+xml") {
		return result, nil
	}
	parseStartedAt := time.Now()
	worker.setPageActivity(runID, page.ID, page.URL, "html_parse")
	logPageStageStarted(ctx, page.URL, "html_parse")
	parsed := pageparse.ParseHTMLPage(response.FinalURL, response.Body)
	logPageProcessingTiming(ctx, page.URL, "html_parse", time.Since(parseStartedAt), false)
	result.stage = "html_parse"
	if ctx.Err() != nil {
		return worker.deferredPageResult(page, "html_parse"), nil
	}
	if !rules.allowsCrawlURL(response.FinalURL) {
		return result, nil
	}
	if rules.allowsIndexing(response.FinalURL, parsed.Noindex, parsed.CanonicalURL) &&
		(strings.TrimSpace(parsed.Title) != "" || strings.TrimSpace(parsed.Text) != "") {
		worker.setPageActivity(runID, page.ID, page.URL, "ingest_queue")
		batch := buildIngestBatch(order, page, response, parsed)
		result.pendingIngestJSON, err = yagocrawlcontract.MarshalIngestBatch(batch)
		if err != nil {
			return pageProcessingResult{state: frontier.StateFailed}, err
		}
		result.stage = "ingest_queue"
	}
	if page.Depth < rules.maximumDepth && !parsed.Nofollow {
		worker.setPageActivity(runID, page.ID, page.URL, "frontier_enqueue")
		logPageStageStarted(ctx, page.URL, "frontier_enqueue")
		result.stage = "frontier_enqueue"
		for _, link := range parsed.Links {
			if ctx.Err() != nil {
				return worker.deferredPageResult(page, result.stage), nil
			}
			if link.NoFollow && !order.Profile.FollowNoFollowLinks {
				continue
			}
			if !rules.allowsCrawlURL(link.URL) {
				continue
			}
			_, _, enqueueErr := worker.frontier.EnqueueRunLimited(
				ctx,
				runID,
				link.URL,
				response.FinalURL,
				page.Depth+1,
				time.Now(),
				rules.maximumHostPages,
				rules.maximumPages,
			)
			if errors.Is(enqueueErr, frontier.ErrRunPageLimit) {
				break
			}
			if enqueueErr != nil && !errors.Is(enqueueErr, frontier.ErrHostPageLimit) &&
				!errors.Is(enqueueErr, frontier.ErrPendingLimit) && !errors.Is(enqueueErr, frontier.ErrStateLimit) {
				if ctx.Err() != nil {
					return worker.deferredPageResult(page, result.stage), nil
				}
				return pageProcessingResult{state: frontier.StateFailed}, enqueueErr
			}
		}
	}
	if ctx.Err() != nil {
		return worker.deferredPageResult(page, result.stage), nil
	}
	slog.DebugContext(ctx, messagePageFetched,
		slog.String("url", page.URL), slog.String("host", page.Host))
	return result, nil
}

func (worker *Worker) retryOrFail(page frontier.Page, fetchError error, status int) frontier.State {
	if page.Attempts < maximumRetryAttempts {
		return frontier.StateDeferred
	}
	if fetchError != nil {
		slog.Warn(messagePageFailed,
			slog.String("url", page.URL), slog.Any("error", fetchError))
	} else {
		slog.Warn(messagePageFailed,
			slog.String("url", page.URL), slog.Int("status", status))
	}
	return frontier.StateFailed
}

func (worker *Worker) enqueueSitemap(
	ctx context.Context,
	body []byte,
	rules runRules,
	runID string,
	depth int,
) error {
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	locations := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return nil
		}
		start, ok := token.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "loc") {
			continue
		}
		var location string
		if err := decoder.DecodeElement(&location, &start); err != nil {
			continue
		}
		if !rules.allowsCrawlURL(location) {
			continue
		}
		if _, _, err := worker.frontier.EnqueueRunLimited(
			ctx,
			runID,
			location,
			"",
			depth,
			time.Now(),
			rules.maximumHostPages,
			rules.maximumPages,
		); err != nil {
			if errors.Is(err, frontier.ErrRunPageLimit) {
				return nil
			}
			if !errors.Is(err, frontier.ErrHostPageLimit) &&
				!errors.Is(err, frontier.ErrPendingLimit) && !errors.Is(err, frontier.ErrStateLimit) {
				return err
			}
		}
		locations++
		if locations >= worker.policy.SitemapURLLimit {
			return nil
		}
	}
}

func runTally(depth frontier.QueueDepth) yagocrawlcontract.CrawlRunTally {
	return yagocrawlcontract.CrawlRunTally{
		Fetched:      depth.Fetched + depth.Indexed,
		Indexed:      depth.Indexed,
		Failed:       depth.Failed,
		RobotsDenied: depth.Blocked,
		Pending:      depth.Pending(),
	}
}

func retryDeadline(page frontier.Page, retryAfter string, now time.Time) time.Time {
	backoff := time.Second << min(page.Attempts-1, 8)
	deadline := now.Add(backoff)
	if retryAfter == "" {
		return deadline
	}
	if seconds, err := time.ParseDuration(retryAfter + "s"); err == nil {
		retryDeadline := now.Add(min(seconds, yagocrawlcontract.MaximumCrawlBackoffDelay))
		if retryDeadline.After(deadline) {
			return retryDeadline
		}
		return deadline
	}
	if parsed, err := http.ParseTime(retryAfter); err == nil {
		maximumDeadline := now.Add(yagocrawlcontract.MaximumCrawlBackoffDelay)
		if parsed.After(maximumDeadline) {
			parsed = maximumDeadline
		}
		if parsed.After(deadline) {
			return parsed
		}
	}
	return deadline
}

func contentURLHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
