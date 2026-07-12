package searchremote

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	remoteQueryResponseByteBudget  = 8 << 20
	remoteQueryDecodedByteBudget   = 8 << 20
	remoteQueryResultEntryBudget   = 1024
	remoteQueryAbstractEntryBudget = 8192
)

var (
	errRemoteSearchBudgetExhausted   = errors.New("remote search response budget exhausted")
	errRemoteSearchAdmissionCanceled = errors.New("remote search admission canceled")
	remoteSearchFetchAdmission       = make(chan struct{}, DefaultConcurrency)
)

type remoteQueryBudget struct {
	responseBytesRemaining   int
	decodedBytesRemaining    int
	resultEntriesRemaining   int
	abstractEntriesRemaining int
}

type peerResourceReduction struct {
	results         []peerSearchResult
	entryLimits     []int
	responseBytes   int
	retainedEntries int
}

type peerAbstractOutcome struct {
	term        yagomodel.Hash
	peer        yagomodel.Seed
	responseErr error
	abstractErr error
	responded   bool
}

type termAbstractReduction struct {
	outcomes        []peerAbstractOutcome
	entryLimits     []int
	abstracts       map[yagomodel.Hash]map[yagomodel.Hash]struct{}
	responseBytes   int
	retainedEntries int
}

func newRemoteQueryBudget() *remoteQueryBudget {
	return &remoteQueryBudget{
		responseBytesRemaining:   remoteQueryResponseByteBudget,
		decodedBytesRemaining:    remoteQueryDecodedByteBudget,
		resultEntriesRemaining:   remoteQueryResultEntryBudget,
		abstractEntriesRemaining: remoteQueryAbstractEntryBudget,
	}
}

func distributedLimits(total, slots, maximum int) []int {
	if slots == 0 {
		return nil
	}
	limits := make([]int, slots)
	share := total / slots
	remainder := total % slots
	for position := range limits {
		limit := share
		if position < remainder {
			limit++
		}
		limits[position] = min(limit, maximum)
	}

	return limits
}

func peerJobsWithResponseLimits(
	requests []peerSearchJob,
	responseBytes int,
) []peerSearchJob {
	limited := slices.Clone(requests)
	limits := distributedLimits(responseBytes, len(limited), remoteSearchBodyCap)
	for position := range limited {
		limited[position].responseBodyLimit = limits[position]
		limited[position].responseBodyLimited = true
	}

	return limited
}

func enterRemoteSearchAdmission(
	ctx context.Context,
	admission chan struct{},
) (func(), error) {
	select {
	case admission <- struct{}{}:
		return func() { <-admission }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf(
			"%w: %w",
			errRemoteSearchAdmissionCanceled,
			ctx.Err(),
		)
	}
}

func (s searcher) remoteSearchAdmission() chan struct{} {
	if s.fetchAdmission != nil {
		return s.fetchAdmission
	}

	return remoteSearchFetchAdmission
}

func (s searcher) reducePeerJobs(
	ctx context.Context,
	requests []peerSearchJob,
	reduce func(peerSearchCompletion),
) {
	if len(requests) == 0 {
		return
	}

	workerCount := max(1, min(s.concurrency, len(requests)))
	jobs := make(chan int)
	completions := make(chan peerSearchCompletion)
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for requestPosition := range jobs {
				completions <- peerSearchCompletion{
					requestPosition: requestPosition,
					result:          s.queryPeerJob(ctx, requests[requestPosition]),
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for requestPosition := range requests {
			jobs <- requestPosition
		}
	}()
	go func() {
		workers.Wait()
		close(completions)
	}()
	for completion := range completions {
		reduce(completion)
	}
}

func (s searcher) queryPeerJobsWithinBudget(
	ctx context.Context,
	requests []peerSearchJob,
	budget *remoteQueryBudget,
) []peerSearchResult {
	limited := peerJobsWithResponseLimits(requests, budget.responseBytesRemaining)
	reduction := peerResourceReduction{
		results: make([]peerSearchResult, len(limited)),
		entryLimits: distributedLimits(
			budget.resultEntriesRemaining,
			len(limited),
			budget.resultEntriesRemaining,
		),
	}
	s.reducePeerJobs(ctx, limited, reduction.accept)
	budget.responseBytesRemaining -= reduction.responseBytes
	budget.resultEntriesRemaining -= reduction.retainedEntries

	return reduction.results
}

func (reduction *peerResourceReduction) accept(completion peerSearchCompletion) {
	result := completion.result
	reduction.responseBytes += result.responseBytes
	limit := reduction.entryLimits[completion.requestPosition]
	retained := min(len(result.response.Resources), limit)
	result.responseIncomplete = result.response.Count > len(result.response.Resources)
	result.resourcesTruncated = len(result.response.Resources) > retained
	result.response = retainedResourceResponse(result.response, retained)
	reduction.retainedEntries += retained
	reduction.results[completion.requestPosition] = result
}

func retainedResourceResponse(
	response yagoproto.SearchResponse,
	retained int,
) yagoproto.SearchResponse {
	return yagoproto.SearchResponse{
		Count:     response.Count,
		Resources: detachedMetadataRows(response.Resources[:retained]),
	}
}

func detachedMetadataRows(rows []yagomodel.URIMetadataRow) []yagomodel.URIMetadataRow {
	detached := make([]yagomodel.URIMetadataRow, len(rows))
	for position, row := range rows {
		properties := make(map[string]string, len(row.Properties))
		for name, value := range row.Properties {
			properties[strings.Clone(name)] = strings.Clone(value)
		}
		detached[position] = yagomodel.URIMetadataRow{Properties: properties}
	}

	return detached
}

func (s searcher) termAbstractsWithinBudget(
	ctx context.Context,
	req searchcore.Request,
	targets []termPeerTargets,
	reputation *reputationSession,
	budget *remoteQueryBudget,
) (map[yagomodel.Hash]map[yagomodel.Hash]struct{}, []searchcore.PartialFailure) {
	requests := abstractSearchJobs(req, targets, s.networkName, s.perPeerTimeout)
	limited := peerJobsWithResponseLimits(requests, budget.responseBytesRemaining)
	reduction := termAbstractReduction{
		outcomes: make([]peerAbstractOutcome, len(limited)),
		entryLimits: distributedLimits(
			budget.abstractEntriesRemaining,
			len(limited),
			budget.abstractEntriesRemaining,
		),
		abstracts: make(map[yagomodel.Hash]map[yagomodel.Hash]struct{}, len(targets)),
	}
	s.reducePeerJobs(ctx, limited, reduction.accept)
	budget.responseBytesRemaining -= reduction.responseBytes
	budget.abstractEntriesRemaining -= reduction.retainedEntries

	return reduction.finish(targets, reputation)
}

func (reduction *termAbstractReduction) accept(completion peerSearchCompletion) {
	result := completion.result
	reduction.responseBytes += result.responseBytes
	outcome := peerAbstractOutcome{
		term:        result.term,
		peer:        result.peer,
		responseErr: result.err,
		responded:   result.err == nil,
	}
	if result.err == nil {
		limit := reduction.entryLimits[completion.requestPosition]
		if limit > 0 {
			urls, err := yagomodel.DecodeSearchIndexAbstractWithLimit(
				result.response.IndexAbstract[result.term],
				limit,
			)
			outcome.abstractErr = err
			if err == nil {
				if reduction.abstracts[result.term] == nil {
					reduction.abstracts[result.term] = map[yagomodel.Hash]struct{}{}
				}
				for _, url := range urls {
					if _, found := reduction.abstracts[result.term][url]; found {
						continue
					}
					reduction.abstracts[result.term][url] = struct{}{}
					reduction.retainedEntries++
				}
			}
		}
	}
	reduction.outcomes[completion.requestPosition] = outcome
}

func (reduction *termAbstractReduction) finish(
	targets []termPeerTargets,
	reputation *reputationSession,
) (map[yagomodel.Hash]map[yagomodel.Hash]struct{}, []searchcore.PartialFailure) {
	successes := make(map[yagomodel.Hash]int, len(targets))
	var failures []searchcore.PartialFailure
	for _, outcome := range reduction.outcomes {
		if !outcome.responded {
			recordPeerFailure(reputation, outcome.peer, outcome.responseErr)
			failures = append(failures, peerFailure(outcome.peer, outcome.responseErr))
			continue
		}
		successes[outcome.term]++
		if outcome.abstractErr != nil {
			reputation.record(outcome.peer, observationOutcome(nil, true))
			failures = append(failures, peerFailure(outcome.peer, outcome.abstractErr))
			continue
		}
		reputation.record(outcome.peer, observationOutcome(nil, false))
	}
	for _, target := range targets {
		if successes[target.term] == 0 {
			failures = append(failures, searchcore.PartialFailure{
				Source: "remote-yacy",
				Reason: "no index abstract responses for " + target.term.String(),
			})
		}
	}

	return reduction.abstracts, failures
}

func recordPeerFailure(
	reputation *reputationSession,
	peer yagomodel.Seed,
	err error,
) {
	if !errors.Is(err, errRemoteSearchBudgetExhausted) &&
		!errors.Is(err, errRemoteSearchAdmissionCanceled) {
		reputation.record(peer, observationOutcome(err, false))
	}
}
