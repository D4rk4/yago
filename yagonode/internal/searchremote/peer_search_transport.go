package searchremote

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func (s searcher) remoteSearch(
	ctx context.Context,
	peer yagomodel.Seed,
	req searchcore.Request,
) (yagoproto.SearchResponse, error) {
	return s.sendRemoteSearch(
		ctx,
		peer,
		remoteSearchRequestWithEvidence(req, s.networkName, s.perPeerTimeout),
	)
}

func (s searcher) sendRemoteSearch(
	ctx context.Context,
	peer yagomodel.Seed,
	searchReq yagoproto.SearchRequest,
) (yagoproto.SearchResponse, error) {
	response, _, err := s.sendRemoteSearchWithinLimit(
		ctx,
		peer,
		searchReq,
		remoteSearchBodyCap,
	)

	return response, err
}

func baseRemoteSearchRequest(
	req searchcore.Request,
	networkName string,
	perPeerTimeout time.Duration,
) yagoproto.SearchRequest {
	return yagoproto.SearchRequest{
		NetworkName: networkName,
		Exclude:     termHashes(req.ExcludedTerms),
		Count:       req.Limit,
		Time:        int(perPeerTimeout / time.Millisecond),
		ContentDom:  yagoproto.SearchContentDomain(req.ContentDomain),
		Language:    req.Language,
		Prefer:      req.PreferMaskFilter,
		Filter:      req.URLMaskFilter,
		SiteHost:    req.SiteHost,
		FileType:    req.FileType,
	}
}

func abstractRemoteSearchRequest(
	req searchcore.Request,
	term yagomodel.Hash,
	networkName string,
	perPeerTimeout time.Duration,
) yagoproto.SearchRequest {
	searchReq := baseRemoteSearchRequest(req, networkName, perPeerTimeout)
	searchReq.Abstracts = yagoproto.SearchAbstracts(term.String())

	return searchReq
}

func secondaryRemoteSearchRequest(
	req searchcore.Request,
	term yagomodel.Hash,
	urls []yagomodel.Hash,
	networkName string,
	perPeerTimeout time.Duration,
) yagoproto.SearchRequest {
	searchReq := baseRemoteSearchRequest(req, networkName, perPeerTimeout)
	searchReq.Query = []yagomodel.Hash{term}
	searchReq.URLs = urls
	searchReq.Count = len(urls)

	return searchReq
}

func remoteSearchRequest(
	req searchcore.Request,
	networkName string,
	perPeerTimeout time.Duration,
) yagoproto.SearchRequest {
	searchReq := baseRemoteSearchRequest(req, networkName, perPeerTimeout)
	searchReq.Query = termHashes(req.Terms)

	return searchReq
}

func remoteSearchRequestWithEvidence(
	req searchcore.Request,
	networkName string,
	perPeerTimeout time.Duration,
) yagoproto.SearchRequest {
	searchReq := remoteSearchRequest(req, networkName, perPeerTimeout)
	requestQueryMatchEvidence(&searchReq, req.Terms)

	return searchReq
}

func requestQueryMatchEvidence(request *yagoproto.SearchRequest, terms []string) {
	request.EvidenceVersion = yagoproto.QueryMatchEvidenceVersion
	request.EvidenceTerms = terms
}

func negotiatedQueryEvidenceTerms(request yagoproto.SearchRequest) []string {
	form := request.Form()
	if form.Get(yagoproto.FieldQueryEvidenceVersion) != "1" {
		return nil
	}

	return append([]string(nil), form[yagoproto.FieldQueryEvidenceTerm]...)
}

func termHashes(terms []string) []yagomodel.Hash {
	hashes := make([]yagomodel.Hash, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		hashes = append(hashes, yagomodel.WordHash(term))
	}

	return hashes
}

func readRemoteSearchResponse(body io.Reader) (yagoproto.SearchResponse, error) {
	response, _, err := readRemoteSearchResponseWithinLimit(body, remoteSearchBodyCap)

	return response, err
}
