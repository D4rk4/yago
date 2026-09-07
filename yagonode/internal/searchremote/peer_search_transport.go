package searchremote

import (
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

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
		Author:      req.Author,
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
	terms []yagomodel.Hash,
	urls []yagomodel.Hash,
	networkName string,
	perPeerTimeout time.Duration,
) yagoproto.SearchRequest {
	searchReq := baseRemoteSearchRequest(req, networkName, perPeerTimeout)
	searchReq.Query = terms
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
	if len(searchReq.Query) > 1 {
		searchReq.Abstracts = yagoproto.SearchAbstractsAuto
	}

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
