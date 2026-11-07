package searchindex

import (
	"strings"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func resultSnippet(
	hit *search.DocumentMatch,
	doc documentstore.Document,
	req SearchRequest,
) string {
	fallback := documentTitle(doc)
	if req.CandidateOnly {
		return snippet(doc.ExtractedText, fallback)
	}
	terms := queryTermWords(req)
	if len(hit.Locations["body"]) == 0 &&
		len(hit.Locations["headings"]) == 0 &&
		len(hit.Locations["anchors"]) == 0 {
		return queryBiasedSnippet(doc.ExtractedText, terms, fallback)
	}
	allowedTerms := analyzedQueryTerms(req)
	if witness, found := fieldSnippetEvidence(
		hit.Locations["body"],
		[]string{doc.ExtractedText},
		allowedTerms,
		req.Fuzzy,
	); found {
		return sentenceSnippet(witness, fallback)
	}
	if witness, found := fieldSnippetEvidence(
		hit.Locations["headings"],
		doc.Headings,
		allowedTerms,
		req.Fuzzy,
	); found {
		return sentenceSnippet(witness, fallback)
	}
	if witness, found := fieldSnippetEvidence(
		hit.Locations["anchors"],
		anchorTexts(doc.Inlinks),
		allowedTerms,
		req.Fuzzy,
	); found {
		return sentenceSnippet(witness, fallback)
	}

	return queryBiasedSnippet(doc.ExtractedText, terms, fallback)
}

func analyzedQueryTerms(req SearchRequest) map[string]struct{} {
	terms := queryTermWords(req)
	out := make(map[string]struct{}, len(terms))
	indexMapping := loadStemmingMapping()
	analyzers := queryAnalyzers(queryAnalyzerText(req))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		out[term] = struct{}{}
		if indexMapping == nil {
			continue
		}
		for _, analyzerName := range analyzers {
			analyzer := indexMapping.AnalyzerNamed(analyzerName)
			if analyzer == nil {
				continue
			}
			for _, token := range analyzer.Analyze([]byte(term)) {
				out[string(token.Term)] = struct{}{}
			}
		}
	}

	return out
}
