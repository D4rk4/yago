package worker

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
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
	rules, err := compileRunRules(order, worker.policy)
	if err != nil {
		execution.tally.Failed = 1
		return execution, nil
	}
	runID := hex.EncodeToString(order.Provenance)
	if _, err := worker.frontier.RecoverExpired(ctx, time.Now()); err != nil {
		return execution, err
	}
	execution.seeds, err = admitOrderSeeds(ctx, worker.frontier, runID, order.Requests, rules)
	if err != nil {
		return execution, err
	}
	return worker.executeFrontier(ctx, order, leaseID, runID, rules, execution, false)
}

func (worker *Worker) executePersistentFrontier(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	leaseID string,
	runID string,
	rules runRules,
) (orderExecutionSummary, error) {
	execution := orderExecutionSummary{}
	if _, err := worker.frontier.RecoverExpired(ctx, time.Now()); err != nil {
		return execution, err
	}
	return worker.executeFrontier(ctx, order, leaseID, runID, rules, execution, true)
}

func (worker *Worker) executeFrontier(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	leaseID string,
	runID string,
	rules runRules,
	execution orderExecutionSummary,
	continuous bool,
) (orderExecutionSummary, error) {
	worker.waitingPageReports.runStarted(runID)
	defer worker.waitingPageReports.runFinished(runID)
	if err := worker.frontier.RebindPendingIngestLease(ctx, runID, leaseID); err != nil {
		return execution, err
	}
	lastProgressLogAt := time.Now()
	lastProgressDepth, lastProgressPendingIngest, err := worker.frontier.ProgressForRun(ctx, runID)
	if err != nil {
		return execution, err
	}
	nextProgressLogAt := lastProgressLogAt.Add(crawlerProgressLogInterval)
	nextLeaseRecovery := time.Now().Add(leaseRecoveryInterval)
	pageResults := make(chan pageExecutionOutcome, yagocrawlcontract.MaximumFetchWorkerConcurrency)
	inFlight := 0
	activeHosts := make(map[string]struct{}, yagocrawlcontract.MaximumFetchWorkerConcurrency)
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
			page, found, err := worker.frontier.ClaimExcludingHosts(
				ctx,
				runID,
				time.Now(),
				activeHosts,
			)
			if err != nil {
				release()
				return execution, err
			}
			if found {
				inFlight++
				activeHosts[page.Host] = struct{}{}
				go func(claimed frontier.Page) {
					err := worker.executeClaimedPage(ctx, order, rules, runID, leaseID, claimed)
					pageResults <- pageExecutionOutcome{host: claimed.Host, err: err}
					release()
				}(page)
				continue
			}
			release()
		}
		depth, pendingIngest, err := worker.frontier.ProgressForRun(ctx, runID)
		if err != nil {
			return execution, err
		}
		progressNow := time.Now()
		if !progressNow.Before(nextProgressLogAt) {
			worker.logCrawlerProgress(
				ctx,
				runID,
				lastProgressDepth,
				depth,
				lastProgressPendingIngest,
				pendingIngest,
				progressNow.Sub(lastProgressLogAt),
			)
			lastProgressLogAt = progressNow
			nextProgressLogAt = progressNow.Add(crawlerProgressLogInterval)
			lastProgressDepth = depth
			lastProgressPendingIngest = pendingIngest
		}
		if depth.Pending() == 0 && inFlight == 0 && pendingIngest == 0 {
			execution.tally = runTally(depth, pendingIngest)
			if !continuous {
				return execution, nil
			}
		}
		if inFlight > 0 {
			select {
			case outcome := <-pageResults:
				inFlight--
				delete(activeHosts, outcome.host)
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
	if worker.urlDenylist.Blocks(page.URL) {
		return pageProcessingResult{state: frontier.StateBlocked, stage: "url_denylist"}, nil
	}
	worker.setPageActivity(runID, page.ID, page.URL, "robots")
	robotsStartedAt := time.Now()
	allowed, err := worker.robots.Allows(ctx, page.URL, order.Profile.IgnoreRobots)
	logPageProcessingTiming(ctx, page.ID, page.Host, "robots", time.Since(robotsStartedAt), err != nil)
	if err != nil {
		if ctx.Err() != nil {
			return worker.deferredPageResult(page, "robots"), nil
		}
		return pageProcessingResult{state: frontier.StateFailed}, err
	}
	result := pageProcessingResult{state: frontier.StateFetched, stage: "robots"}
	if !allowed {
		slog.DebugContext(ctx, messageRobotsDenied,
			slog.String("pageId", page.ID), slog.String("host", page.Host))
		return pageProcessingResult{state: frontier.StateBlocked}, nil
	}
	worker.setPageActivity(runID, page.ID, page.URL, "http_fetch")
	response, err := worker.fetchPage(ctx, page.URL)
	if err != nil {
		return pageProcessingResult{
			state:   worker.retryOrFail(page, err, 0),
			retryAt: retryDeadline(page, "", time.Now()),
			stage:   "http_fetch",
		}, nil
	}
	if worker.urlDenylist.Blocks(response.FinalURL) {
		return pageProcessingResult{state: frontier.StateBlocked, stage: "url_denylist"}, nil
	}
	result.stage = "http_fetch"
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		batch := buildRemovalBatch(order, page, response)
		result.pendingIngestJSON, err = yagocrawlcontract.MarshalIngestBatch(batch)
		if err != nil {
			return pageProcessingResult{state: frontier.StateFailed}, err
		}
		result.stage = "document_remove"
		result.outcome = frontier.PageOutcome{Class: frontier.OutcomeRemovalMutation}
		return result, nil
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return pageProcessingResult{
			state:       worker.retryOrFail(page, nil, response.StatusCode),
			retryAt:     retryDeadline(page, response.RetryAfter, time.Now()),
			rateLimited: response.StatusCode == http.StatusTooManyRequests,
		}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		slog.WarnContext(ctx, messagePageFailed,
			slog.String("pageId", page.ID), slog.String("host", page.Host),
			slog.Int("status", response.StatusCode))
		return pageProcessingResult{state: frontier.StateFailed}, nil
	}
	mediaType, _, _ := mime.ParseMediaType(response.ContentType)
	isSitemap := strings.EqualFold(mediaType, "application/xml") ||
		strings.EqualFold(mediaType, "text/xml") ||
		strings.Contains(strings.ToLower(response.ContentType), "sitemap")
	if isSitemap {
		if err := worker.enqueueSitemap(ctx, response.Body, rules, runID, page.Depth+1); err != nil {
			if ctx.Err() != nil {
				return worker.deferredPageResult(page, "sitemap"), nil
			}
			return pageProcessingResult{state: frontier.StateFailed}, err
		}
		result.outcome = classifyPageIndexability(mediaType, true, true, true, false)
		return result, nil
	}
	if !strings.EqualFold(mediaType, "text/html") && !strings.EqualFold(mediaType, "application/xhtml+xml") {
		result.outcome = classifyPageIndexability(mediaType, false, true, true, false)
		return result, nil
	}
	parseStartedAt := time.Now()
	worker.setPageActivity(runID, page.ID, page.URL, "html_parse")
	parsed := pageparse.ParseHTMLPage(response.FinalURL, response.Body)
	logPageProcessingTiming(ctx, page.ID, page.Host, "html_parse", time.Since(parseStartedAt), false)
	result.stage = "html_parse"
	if ctx.Err() != nil {
		return worker.deferredPageResult(page, "html_parse"), nil
	}
	result.outcome = classifyPageIndexability(
		mediaType,
		false,
		rules.allowsCrawlURL(response.FinalURL),
		rules.allowsIndexing(response.FinalURL, parsed.Noindex, parsed.CanonicalURL),
		strings.TrimSpace(parsed.Title) != "" || strings.TrimSpace(parsed.Text) != "",
	)
	if result.outcome.Class == frontier.OutcomeDocumentCandidate {
		worker.setPageActivity(runID, page.ID, page.URL, "ingest_queue")
		batch := buildIngestBatch(order, page, response, parsed)
		result.pendingIngestJSON, err = yagocrawlcontract.MarshalIngestBatch(batch)
		if err != nil {
			return pageProcessingResult{state: frontier.StateFailed}, err
		}
		result.stage = "ingest_queue"
		result.outcome = frontier.PageOutcome{Class: frontier.OutcomeDocumentCandidate}
	}
	if page.Depth < rules.maximumDepth && !parsed.Nofollow {
		worker.setPageActivity(runID, page.ID, page.URL, "frontier_enqueue")
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
			if worker.urlDenylist.Blocks(link.URL) {
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
		slog.String("pageId", page.ID),
		slog.String("host", page.Host),
		slog.String("outcome", result.outcome.Class),
		slog.String("reason", result.outcome.Reason))
	return result, nil
}

func (worker *Worker) retryOrFail(page frontier.Page, fetchError error, status int) frontier.State {
	if page.Attempts < maximumRetryAttempts {
		return frontier.StateDeferred
	}
	if fetchError != nil {
		slog.Warn(messagePageFailed,
			slog.String("pageId", page.ID), slog.String("host", page.Host),
			slog.String("errorType", crawlerFetchErrorType(fetchError)),
			slog.Bool("timeout", errors.Is(fetchError, context.DeadlineExceeded)))
	} else {
		slog.Warn(messagePageFailed,
			slog.String("pageId", page.ID), slog.String("host", page.Host),
			slog.Int("status", status))
	}
	return frontier.StateFailed
}

func runTally(depth frontier.QueueDepth, pendingIngest uint64) yagocrawlcontract.CrawlRunTally {
	outcomeCounts := make([]yagocrawlcontract.CrawlOutcomeCount, 0, len(depth.OutcomeCounts))
	for _, count := range depth.OutcomeCounts {
		outcomeCounts = append(outcomeCounts, yagocrawlcontract.CrawlOutcomeCount{
			Class: count.Class, Reason: count.Reason, Pages: count.Pages,
		})
	}
	sort.Slice(outcomeCounts, func(left, right int) bool {
		if outcomeCounts[left].Class == outcomeCounts[right].Class {
			return outcomeCounts[left].Reason < outcomeCounts[right].Reason
		}
		return outcomeCounts[left].Class < outcomeCounts[right].Class
	})
	return yagocrawlcontract.CrawlRunTally{
		Fetched:            depth.Fetched + depth.Indexed,
		Indexed:            depth.Indexed,
		Failed:             depth.Failed,
		RobotsDenied:       depth.Blocked,
		Pending:            depth.Pending(),
		DocumentCandidates: depth.DocumentCandidates,
		RemovalMutations:   depth.RemovalMutations,
		NotIndexable:       depth.NotIndexable,
		NodeRejected:       depth.NodeRejected,
		PendingIngest:      pendingIngest,
		OutcomeCounts:      outcomeCounts,
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
