package documentsearch

import (
	"fmt"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
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

func searchCriteriaFromRequest(req yacyproto.SearchRequest) (searchCriteria, error) {
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

	return searchCriteria{
		terms:              req.Query,
		excludedTerms:      req.Exclude,
		requiredDocuments:  req.URLs,
		maxResults:         maxResults,
		maxTermSpread:      req.MaxDist,
		timeLimit:          timeLimit,
		reporting:          matchReportingFromRequest(req),
		contentKind:        contentKindFromDomain(req.ContentDom),
		strictContentKind:  req.StrictContentDom,
		requiredProperties: required,
		// Deliberate divergence from YaCy: only the /language/ modifier filters; the
		// plain language field drives YaCy's ranking boost, which this node omits.
		language: operators.Language,
		siteHash: siteHash,
	}, nil
}

func contentKindFromDomain(domain yacyproto.SearchContentDomain) contentKind {
	switch domain {
	case yacyproto.ContentDomainImage:
		return imageContent
	case yacyproto.ContentDomainAudio:
		return audioContent
	case yacyproto.ContentDomainVideo:
		return videoContent
	case yacyproto.ContentDomainApp:
		return applicationContent
	default:
		return anyContent
	}
}

func requiredProperties(encoded string) (yacymodel.Bitfield, error) {
	if encoded == "" {
		return nil, nil
	}
	required, err := yacymodel.DecodeBitfield(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode required properties: %w", err)
	}
	if required.AllSet(yacymodel.RWIFlagBitCount) {
		return nil, nil
	}

	return required, nil
}

func resolveSiteHash(req yacyproto.SearchRequest, operators queryOperators) (string, error) {
	if req.SiteHash != "" {
		return req.SiteHash, nil
	}
	host := firstNonEmpty(operators.SiteHost, req.SiteHost)
	if host == "" {
		return "", nil
	}
	hash, err := yacymodel.HashURLHost(host)
	if err != nil {
		return "", fmt.Errorf("site hash: %w", err)
	}
	hostHash, err := hash.HostHash()
	if err != nil {
		return "", fmt.Errorf("site hash: %w", err)
	}

	return hostHash, nil
}

func matchReportingFromRequest(req yacyproto.SearchRequest) matchReporting {
	switch req.Abstracts {
	case "":
		return matchReporting{mode: reportNoMatches}
	case yacyproto.SearchAbstractsAuto:
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
