package searchindex

import (
	"strings"
	"unicode"

	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

func bleveQueryWithIncludeDomainCandidates(
	main blevequery.Query,
	domains []string,
) blevequery.Query {
	domainCandidates := bleveIncludeDomainCandidateQuery(domains)
	if domainCandidates == nil {
		return main
	}

	return bleve.NewConjunctionQuery(main, domainCandidates)
}

func bleveIncludeDomainCandidateQuery(domains []string) blevequery.Query {
	candidates := make([]blevequery.Query, 0, len(domains))
	for _, domain := range domains {
		domain = strings.Trim(strings.TrimSpace(domain), ".")
		if domain == "" {
			continue
		}
		if !containsURLAnalyzerToken(domain) {
			return nil
		}
		candidates = append(candidates, fieldMatch("url", domain, 0))
	}
	if len(candidates) == 0 {
		return nil
	}

	query := bleve.NewDisjunctionQuery(candidates...)
	query.SetMin(1)

	return query
}

func containsURLAnalyzerToken(text string) bool {
	for _, symbol := range text {
		if unicode.IsLetter(symbol) || unicode.IsNumber(symbol) || unicode.IsMark(symbol) {
			return true
		}
	}

	return false
}
