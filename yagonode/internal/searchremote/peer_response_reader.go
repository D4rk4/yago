package searchremote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
	"github.com/D4rk4/yago/yagoproto"
)

func (s searcher) sendRemoteSearchWithinLimit(
	ctx context.Context,
	peer yagomodel.Seed,
	searchReq yagoproto.SearchRequest,
	responseBodyLimit int,
	callBudgets ...*outboundCallBudget,
) (yagoproto.SearchResponse, int, error) {
	if s.selfSeed != nil {
		searchReq.MySeed = yagomodel.Some(s.selfSeed(ctx))
	}
	targets, err := peer.ProtocolEndpoints(yagoproto.PathSearch, s.preferHTTPS)
	if err != nil {
		return yagoproto.SearchResponse{}, 0, fmt.Errorf(
			"%w: target: %w",
			errRemoteSearchFailed,
			err,
		)
	}

	var lastErr error
	responseBytes := 0
	for _, target := range targets {
		response, readBytes, err := s.sendRemoteSearchToWithinLimit(
			ctx,
			target,
			searchReq,
			responseBodyLimit-responseBytes,
			callBudgets...,
		)
		responseBytes += readBytes
		if err == nil {
			return response, responseBytes, nil
		}
		lastErr = err
		if !errors.Is(err, errRemoteSearchTransport) {
			break
		}
	}

	return yagoproto.SearchResponse{}, responseBytes, lastErr
}

func (s searcher) sendRemoteSearchToWithinLimit(
	ctx context.Context,
	target *url.URL,
	searchReq yagoproto.SearchRequest,
	responseBodyLimit int,
	callBudgets ...*outboundCallBudget,
) (yagoproto.SearchResponse, int, error) {
	target.RawQuery = searchReq.Form().Encode()

	httpReq, err := newRemoteSearchRequest(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return yagoproto.SearchResponse{}, 0, fmt.Errorf(
			"%w: request: %w",
			errRemoteSearchFailed,
			err,
		)
	}
	if trace, ok := tracectx.FromContext(ctx); ok {
		httpReq.Header.Set(tracectx.Header, trace.Child().Header())
	}
	if !acquireOutboundCall(callBudgets...) {
		return yagoproto.SearchResponse{}, 0, errRemoteSearchBudgetExhausted
	}
	release, err := enterRemoteSearchAdmission(ctx, s.remoteSearchAdmission())
	if err != nil {
		return yagoproto.SearchResponse{}, 0, fmt.Errorf(
			"%w: admission: %w",
			errRemoteSearchFailed,
			err,
		)
	}
	defer release()
	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		return yagoproto.SearchResponse{}, 0, fmt.Errorf(
			"%w: %w: %w",
			errRemoteSearchFailed,
			errRemoteSearchTransport,
			err,
		)
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != http.StatusOK {
		return yagoproto.SearchResponse{}, 0, fmt.Errorf(
			"%w: status %d",
			errRemoteSearchFailed,
			httpResp.StatusCode,
		)
	}

	return readRemoteSearchResponseWithinLimit(httpResp.Body, responseBodyLimit)
}

func readRemoteSearchResponseWithinLimit(
	body io.Reader,
	responseBodyLimit int,
) (yagoproto.SearchResponse, int, error) {
	raw, err := io.ReadAll(io.LimitReader(body, int64(responseBodyLimit)+1))
	responseBytes := min(len(raw), responseBodyLimit)
	if err != nil {
		return yagoproto.SearchResponse{}, responseBytes, fmt.Errorf(
			"%w: read response: %w",
			errRemoteSearchFailed,
			err,
		)
	}
	if len(raw) > responseBodyLimit {
		cause := errRemoteSearchInvalidResult
		if responseBodyLimit < remoteSearchBodyCap {
			cause = errRemoteSearchBudgetExhausted
		}
		return yagoproto.SearchResponse{}, responseBytes, fmt.Errorf(
			"%w: %w: response too large",
			errRemoteSearchFailed,
			cause,
		)
	}
	msg, _ := yagomodel.ParseMessage(string(raw))
	parsed, err := yagoproto.ParseSearchResponse(msg)
	if err != nil {
		return yagoproto.SearchResponse{}, responseBytes, fmt.Errorf(
			"%w: %w: search response: %w",
			errRemoteSearchFailed,
			errRemoteSearchInvalidResult,
			err,
		)
	}

	return parsed, responseBytes, nil
}
