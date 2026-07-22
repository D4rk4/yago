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

type remoteSearchRequestLimits struct {
	responseBodyLimit int
	transportAttempts *outboundCallBudget
	callBudgets       []*outboundCallBudget
}

func (s searcher) sendRemoteSearchWithinLimit(
	ctx context.Context,
	peer yagomodel.Seed,
	searchReq yagoproto.SearchRequest,
	limits remoteSearchRequestLimits,
) (yagoproto.SearchResponse, int, error) {
	var self yagomodel.Seed
	if s.selfSeed != nil {
		self = s.selfSeed(ctx)
		searchReq.MySeed = yagomodel.Some(self)
		searchReq.Iam = self.Hash.String()
	}
	if s.access.Mode == yagoproto.NetworkAuthenticationSaltedMagic {
		if self.Hash == "" {
			return yagoproto.SearchResponse{}, 0, fmt.Errorf(
				"%w: missing self identity",
				errRemoteSearchFailed,
			)
		}
		access := s.access
		access.Self = self.Hash
		form := searchReq.Form()
		if err := s.signNetworkForm(access, form); err != nil {
			return yagoproto.SearchResponse{}, 0, fmt.Errorf(
				"%w: %w",
				errRemoteSearchFailed,
				err,
			)
		}
		searchReq.Iam = form.Get(yagoproto.FieldIam)
		searchReq.Key = form.Get(yagoproto.FieldKey)
		searchReq.MagicMD5 = form.Get(yagoproto.FieldMagicMD5)
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
	if !acquireOutboundCall(limits.callBudgets...) {
		return yagoproto.SearchResponse{}, 0, errRemoteSearchBudgetExhausted
	}
	for position, target := range targets {
		attemptContext, cancel := remoteSearchEndpointAttemptContext(
			ctx,
			len(targets)-position,
		)
		response, readBytes, err := s.sendRemoteSearchToWithoutCallBudget(
			attemptContext,
			target,
			searchReq,
			limits.responseBodyLimit-responseBytes,
			limits.transportAttempts,
		)
		cancel()
		responseBytes += readBytes
		if err == nil {
			s.observeReceivedResponse(ctx, response)
			return response, responseBytes, nil
		}
		lastErr = err
		if !errors.Is(err, errRemoteSearchTransport) {
			break
		}
	}

	return yagoproto.SearchResponse{}, responseBytes, lastErr
}

func (s searcher) sendRemoteSearchToWithoutCallBudget(
	ctx context.Context,
	target *url.URL,
	searchReq yagoproto.SearchRequest,
	responseBodyLimit int,
	transportAttempts ...*outboundCallBudget,
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
	release, err := enterRemoteSearchAdmission(ctx, s.remoteSearchAdmission())
	if err != nil {
		return yagoproto.SearchResponse{}, 0, fmt.Errorf(
			"%w: admission: %w",
			errRemoteSearchFailed,
			err,
		)
	}
	defer release()
	if !acquireOutboundCall(transportAttempts...) {
		return yagoproto.SearchResponse{}, 0, errRemoteSearchBudgetExhausted
	}
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
			"%w: %w: read response: %w",
			errRemoteSearchFailed,
			errRemoteSearchTransport,
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
	if err := validateRemoteResourceIntegrity(parsed); err != nil {
		return yagoproto.SearchResponse{}, responseBytes, fmt.Errorf(
			"%w: %w: search response: %w",
			errRemoteSearchFailed,
			errRemoteSearchInvalidResult,
			err,
		)
	}
	if err := validateRemoteResourceWordReferences(parsed.Resources); err != nil {
		return yagoproto.SearchResponse{}, responseBytes, fmt.Errorf(
			"%w: %w: search response: %w",
			errRemoteSearchFailed,
			errRemoteSearchInvalidResult,
			err,
		)
	}

	return parsed, responseBytes, nil
}
