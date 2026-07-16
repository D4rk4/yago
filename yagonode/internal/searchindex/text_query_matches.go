package searchindex

import (
	"strings"

	"github.com/blevesearch/bleve/v2/analysis"
)

type TextQueryMatch struct {
	Start int
	End   int
}

type AnalyzedQueryTerms struct {
	analyzer  analysis.Analyzer
	targets   map[string]struct{}
	available bool
}

func NewAnalyzedQueryTerms(terms []string, analyzerName string) AnalyzedQueryTerms {
	analyzer := storedEvidenceAnalyzer(analyzerName)
	if len(terms) == 0 || analyzer == nil {
		return AnalyzedQueryTerms{}
	}
	targets := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		for _, token := range analyzer.Analyze([]byte(term)) {
			targets[string(token.Term)] = struct{}{}
		}
	}

	return AnalyzedQueryTerms{analyzer: analyzer, targets: targets, available: true}
}

func (query AnalyzedQueryTerms) TextMatches(text string) []TextQueryMatch {
	if !query.available {
		return nil
	}
	matches := make([]TextQueryMatch, 0, len(query.targets))
	if len(query.targets) == 0 || text == "" {
		return matches
	}
	for _, token := range query.analyzer.Analyze([]byte(text)) {
		if _, found := query.targets[string(token.Term)]; !found || token.Start < 0 ||
			token.End <= token.Start || token.End > len(text) {
			continue
		}
		matches = append(matches, TextQueryMatch{Start: token.Start, End: token.End})
	}
	return matches
}
