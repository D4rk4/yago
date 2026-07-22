package documentsearch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type searchEndpoint struct {
	identity       nodeidentity.Identity
	searcher       searcher
	gate           *httpguard.IntakeGate
	analyzerRecall negotiatedAnalyzerRecallSource
	evidence       queryMatchEvidenceSource
	potentialPeers PotentialPeerObserver
}

func (e searchEndpoint) Serve(
	ctx context.Context,
	req yagoproto.SearchRequest,
) (yagoproto.SearchResponse, error) {
	resp := yagoproto.SearchResponse{}

	// Distributed-search DoS protection (YaCy 1.0): a flood of concurrent
	// remote searches gets empty-but-valid responses instead of exhausting
	// the node.
	release, ok := e.gate.TryAcquire()
	if !ok {
		slog.DebugContext(ctx, "inbound remote search shed: all slots busy")

		return resp, nil
	}
	defer release()

	if e.identity.Authenticates(
		req.NetworkName,
		req.NetworkNamePresent,
		req.Key,
		req.Iam,
		req.MagicMD5,
	) {
		e.observePotentialPeer(ctx, req)
		criteria, err := searchCriteriaFromRequest(req)
		if err != nil {
			return yagoproto.SearchResponse{}, fmt.Errorf("search criteria: %w", err)
		}
		if ignored := ignoredOptionNames(req); len(ignored) != 0 {
			slog.DebugContext(ctx, "ignoring accepted search options",
				slog.Any("options", ignored),
			)
		}
		searchStarted := time.Now()
		searchCtx := ctx
		if criteria.timeLimit > 0 {
			var cancel func()
			searchCtx, cancel = context.WithTimeout(ctx, criteria.timeLimit)
			defer cancel()
		}

		result, err := e.searcher.search(searchCtx, criteria)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) &&
				errors.Is(searchCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
				resp.SearchTime = int(time.Since(searchStarted) / time.Millisecond)
				slog.DebugContext(ctx, msgRemoteSearchDeadline,
					slog.Int("searchTimeMilliseconds", resp.SearchTime),
				)
			} else {
				return yagoproto.SearchResponse{}, fmt.Errorf("search: %w", err)
			}
		} else {
			result, err = e.analyzerRecall.merge(searchCtx, req, result)
			if err != nil {
				slog.DebugContext(ctx, msgNegotiatedAnalyzerRecallUnavailable,
					slog.Any("error", err),
				)
			}
			result.resources = resourcesWithDefaultWordReferences(result.resources)
			resp.SearchTime = int(result.searchDuration / time.Millisecond)
			resp.References = strings.Join(result.topics, ",")
			resp.JoinCount = result.totalDocumentsMatchingEveryTerm
			resp.Count = len(result.resources)
			resp.Resources = result.resources
			resp.IndexCount = result.totalMatchesPerTerm
			resp.IndexAbstract = result.documentsMatchingEachReportedTerm
			resp.ResourceEvidence = e.evidence.resources(searchCtx, req, result.resources)
			resp.SearchTime = int(time.Since(searchStarted) / time.Millisecond)
		}
	}

	slog.DebugContext(ctx, "search completed",
		slog.Int("resultCount", resp.Count),
		slog.Int("joinCount", resp.JoinCount),
	)

	return resp, nil
}
