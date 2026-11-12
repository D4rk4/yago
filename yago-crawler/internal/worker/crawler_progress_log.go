package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

const (
	crawlerProgressLogInterval       = time.Minute
	messageCrawlerProgress           = "crawler progress interval completed"
)

func (worker *Worker) logCrawlerProgress(
	ctx context.Context,
	runID string,
	previous frontier.QueueDepth,
	current frontier.QueueDepth,
	previousPendingIngest uint64,
	currentPendingIngest uint64,
	elapsed time.Duration,
) {
	window := worker.activity.takeWindow(time.Now())
	requestWindow := window.Elapsed
	if requestWindow <= 0 {
		requestWindow = elapsed
	}
	fetchedBefore := previous.Fetched + previous.Indexed
	fetchedNow := current.Fetched + current.Indexed
	slog.InfoContext(ctx, messageCrawlerProgress,
		slog.String("workerId", worker.workerID),
		slog.String("runId", runID),
		slog.Duration("window", elapsed),
		slog.Duration("requestWindow", requestWindow),
		slog.Float64("pagesFetchedPerMinute", pagesPerMinute(fetchedBefore, fetchedNow, elapsed)),
		slog.Float64("pagesIndexedPerMinute", pagesPerMinute(previous.Indexed, current.Indexed, elapsed)),
		slog.Float64("pagesFailedPerMinute", pagesPerMinute(previous.Failed, current.Failed, elapsed)),
		slog.Float64("pagesBlockedPerMinute", pagesPerMinute(previous.Blocked, current.Blocked, elapsed)),
		slog.Float64("pageFetchRequestsStartedPerMinute", crawlerActivityRatePerMinute(window.Page.started, requestWindow)),
		slog.Float64("pageFetchRequestsCompletedPerMinute", crawlerActivityRatePerMinute(window.Page.completed, requestWindow)),
		slog.Uint64("pageFetchRequestsStarted", window.Page.started),
		slog.Uint64("pageFetchRequestsCompleted", window.Page.completed),
		slog.Uint64("pageFetchRequestFailures", window.Page.failed),
		slog.Uint64("pageHttpResponses2xx", window.Page.responses2xx),
		slog.Uint64("pageHttpResponses3xx", window.Page.responses3xx),
		slog.Uint64("pageHttpResponses4xx", window.Page.responses4xx),
		slog.Uint64("pageHttpResponses5xx", window.Page.responses5xx),
		slog.Uint64("pageHttpResponsesOther", window.Page.responsesOther),
		slog.Uint64("pageHostAdmissionFailures", window.Page.admissionFailures),
		slog.Uint64("robotsFetchRequestsStarted", window.Robots.started),
		slog.Uint64("robotsFetchRequestsCompleted", window.Robots.completed),
		slog.Uint64("robotsFetchRequestFailures", window.Robots.failed),
		slog.Uint64("robotsHttpResponses2xx", window.Robots.responses2xx),
		slog.Uint64("robotsHttpResponses3xx", window.Robots.responses3xx),
		slog.Uint64("robotsHttpResponses4xx", window.Robots.responses4xx),
		slog.Uint64("robotsHttpResponses5xx", window.Robots.responses5xx),
		slog.Uint64("robotsHttpResponsesOther", window.Robots.responsesOther),
		slog.Uint64("robotsHostAdmissionFailures", window.Robots.admissionFailures),
		slog.Int64("pageHostAdmissionWaitMeanMs", meanMilliseconds(window.Page.admissionWaitTotal, window.Page.admissionWaitSamples)),
		slog.Int64("pageHostAdmissionWaitMaxMs", int64(window.Page.admissionWaitMaximum/time.Millisecond)),
		slog.Int64("robotsHostAdmissionWaitMeanMs", meanMilliseconds(window.Robots.admissionWaitTotal, window.Robots.admissionWaitSamples)),
		slog.Int64("robotsHostAdmissionWaitMaxMs", int64(window.Robots.admissionWaitMaximum/time.Millisecond)),
		slog.Int64("pageHttpRequestMeanMs", meanMilliseconds(window.Page.requestTimeTotal, window.Page.completed)),
		slog.Int64("pageHttpRequestMaxMs", int64(window.Page.requestTimeMaximum/time.Millisecond)),
		slog.Int64("robotsHttpRequestMeanMs", meanMilliseconds(window.Robots.requestTimeTotal, window.Robots.completed)),
		slog.Int64("robotsHttpRequestMaxMs", int64(window.Robots.requestTimeMaximum/time.Millisecond)),
		slog.Uint64("nodeIngestMutationsSent", window.IngestMutationsSent),
		slog.Uint64("nodeIngestSendFailures", window.IngestSendFailures),
		slog.Uint64("nodeIngestAcksAccepted", window.NodeAcksAccepted),
		slog.Uint64("nodeIngestAcksRejected", window.NodeAcksRejected),
		slog.Uint64("nodeIngestAckFailures", window.NodeAckFailures),
		slog.Int64("nodeIngestAckMeanMs", meanMilliseconds(window.NodeAckTimeTotal, window.NodeAcksAccepted+window.NodeAcksRejected+window.NodeAckFailures)),
		slog.Int64("nodeIngestAckMaxMs", int64(window.NodeAckTimeMaximum/time.Millisecond)),
		slog.Uint64("activeHttpRequests", uint64(worker.active.Load())),
		slog.Uint64("frontierQueued", current.Queued),
		slog.Uint64("frontierFetching", current.Fetching),
		slog.Uint64("frontierDeferred", current.Deferred),
		slog.Uint64("frontierFetched", current.Fetched),
		slog.Uint64("frontierIndexed", current.Indexed),
		slog.Uint64("documentCandidates", current.DocumentCandidates),
		slog.Uint64("removalMutations", current.RemovalMutations),
		slog.Uint64("notIndexable", current.NotIndexable),
		slog.Uint64("nodeRejected", current.NodeRejected),
		slog.Any("outcomeCounts", current.OutcomeCounts),
		slog.Uint64("frontierFailed", current.Failed),
		slog.Uint64("frontierBlocked", current.Blocked),
		slog.Uint64("frontierCancelled", current.Cancelled),
		slog.Uint64("pendingIngest", currentPendingIngest),
		slog.Int64("pendingIngestDelta", signedDifference(currentPendingIngest, previousPendingIngest)),
		slog.Any("hosts", window.Hosts),
		slog.Uint64("untrackedHostRequests", window.UntrackedHostRequests),
		slog.Uint64("omittedHostSummaries", window.OmittedHostSummaries),
	)
}

func pagesPerMinute(previous uint64, current uint64, elapsed time.Duration) float64 {
	if current < previous || elapsed <= 0 {
		return 0
	}
	return float64(current-previous) * float64(time.Minute) / float64(elapsed)
}

func crawlerActivityRatePerMinute(events uint64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	return float64(events) * float64(time.Minute) / float64(elapsed)
}

func meanMilliseconds(total time.Duration, samples uint64) int64 {
	if samples == 0 {
		return 0
	}
	return int64(total / time.Duration(samples) / time.Millisecond)
}

func signedDifference(current uint64, previous uint64) int64 {
	if current >= previous {
		return int64(current - previous)
	}
	return -int64(previous - current)
}
