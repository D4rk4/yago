package documentsearch

import (
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/sitehost"
	"github.com/D4rk4/yago/yagoproto"
)

func searchCriteriaFromRequest(req yagoproto.SearchRequest) (searchCriteria, error) {
	operators := parseQueryOperators(req.Modifier)
	siteHashes, err := resolveSiteHashes(req, operators)
	if err != nil {
		return searchCriteria{}, err
	}
	maxResults := receiverSearchCount(req.Count)
	timeLimit := receiverSearchTime(req.Time)
	required, err := requiredProperties(req.Constraint)
	if err != nil {
		return searchCriteria{}, err
	}
	metadata, err := metadataConstraintsFromRequest(req, operators)
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
		language:   operators.Language,
		siteHashes: siteHashes,
		metadata:   metadata,
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

func resolveSiteHashes(req yagoproto.SearchRequest, operators queryOperators) ([]string, error) {
	if operators.SiteHost != "" {
		return hashesForSiteHost(operators.SiteHost)
	}
	if req.SiteHash != "" {
		return []string{req.SiteHash}, nil
	}
	host := req.SiteHost
	if host == "" {
		return nil, nil
	}

	return hashesForSiteHost(host)
}

func hashesForSiteHost(host string) ([]string, error) {
	equivalents := sitehost.Equivalents(host)
	if len(equivalents) == 0 {
		_, err := yagomodel.HashURLHost(host)

		return nil, fmt.Errorf("site hash: %w", err)
	}
	hashes := make([]string, 0, len(equivalents))
	for _, equivalent := range equivalents {
		hash, err := yagomodel.HashURLHost(equivalent)
		if err != nil {
			return nil, fmt.Errorf("site hash: %w", err)
		}
		hostHash, _ := hash.HostHash()
		hashes = append(hashes, hostHash)
	}

	return hashes, nil
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
