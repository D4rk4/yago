package documentsearch

import (
	"fmt"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	operatorLanguagePrefix = "/language/"
	operatorSitePrefix     = "site:"
	operatorLanguageLength = 2
)

type queryOperators struct {
	Language string
	SiteHost string
}

func parseQueryOperators(query string) queryOperators {
	var parsed queryOperators
	for token := range strings.FieldsSeq(query) {
		switch {
		case strings.HasPrefix(token, operatorLanguagePrefix):
			if code := token[len(operatorLanguagePrefix):]; len(code) == operatorLanguageLength {
				parsed.Language = strings.ToLower(code)
			}
		case strings.HasPrefix(token, operatorSitePrefix):
			parsed.SiteHost = token[len(operatorSitePrefix):]
		}
	}

	return parsed
}

func searchCriteriaFromRequest(req yagoproto.SearchRequest) (searchCriteria, error) {
	operators := parseQueryOperators(req.Modifier)
	siteHash, err := resolveSiteHash(req, operators)
	if err != nil {
		return searchCriteria{}, err
	}
	maxResults := req.Count
	if maxResults <= 0 {
		maxResults = defaultSearchCount
	}
	timeLimit := time.Duration(req.Time) * time.Millisecond
	if timeLimit <= 0 {
		timeLimit = defaultSearchTime
	}
	required, err := requiredProperties(req.Constraint)
	if err != nil {
		return searchCriteria{}, err
	}

	reporting := matchReportingFromRequest(req)

	return searchCriteria{
		terms:              req.Query,
		excludedTerms:      req.Exclude,
		requiredDocuments:  req.URLs,
		maxResults:         maxResults,
		maxTermSpread:      req.MaxDist,
		timeLimit:          timeLimit,
		reporting:          reporting,
		contentKind:        contentKindFromDomain(req.ContentDom),
		strictContentKind:  req.StrictContentDom,
		requiredProperties: required,
		// A peer that requests no index abstracts never reads per-term totals, so
		// the scan may stop at the cap; abstract modes keep it exhaustive.
		allowEarlyTermination: !reporting.reportsTermCounts(),
		// Deliberate divergence from YaCy: only the /language/ modifier filters; the
		// plain language field drives YaCy's ranking boost, which this node omits.
		language: operators.Language,
		siteHash: siteHash,
	}, nil
}

func contentKindFromDomain(domain yagoproto.SearchContentDomain) contentKind {
	switch domain {
	case yagoproto.ContentDomainImage:
		return imageContent
	case yagoproto.ContentDomainAudio:
		return audioContent
	case yagoproto.ContentDomainVideo:
		return videoContent
	case yagoproto.ContentDomainApp:
		return applicationContent
	default:
		return anyContent
	}
}

func requiredProperties(encoded string) (yagomodel.Bitfield, error) {
	if encoded == "" {
		return nil, nil
	}
	required, err := yagomodel.DecodeBitfield(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode required properties: %w", err)
	}
	if required.AllSet(yagomodel.RWIFlagBitCount) {
		return nil, nil
	}

	return required, nil
}

func resolveSiteHash(req yagoproto.SearchRequest, operators queryOperators) (string, error) {
	if req.SiteHash != "" {
		return req.SiteHash, nil
	}
	host := firstNonEmpty(operators.SiteHost, req.SiteHost)
	if host == "" {
		return "", nil
	}
	hash, err := yagomodel.HashURLHost(host)
	if err != nil {
		return "", fmt.Errorf("site hash: %w", err)
	}
	hostHash, _ := hash.HostHash()
	return hostHash, nil
}

func matchReportingFromRequest(req yagoproto.SearchRequest) matchReporting {
	switch req.Abstracts {
	case "":
		return matchReporting{mode: reportNoMatches}
	case yagoproto.SearchAbstractsAuto:
		return matchReporting{mode: reportTermWithMostMatches}
	default:
		return matchReporting{mode: reportRequestedTerms, terms: req.Abstracts.Hashes()}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
