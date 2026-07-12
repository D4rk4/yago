package searchindex

import (
	"strings"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type snippetEvidence struct {
	text       string
	arrayIndex uint64
	start      int
}

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
	if evidence, found := fieldSnippetEvidence(
		hit,
		"body",
		[]string{doc.ExtractedText},
		allowedTerms,
		req.Fuzzy,
	); found {
		return locationBiasedSnippet(evidence, fallback)
	}
	if evidence, found := fieldSnippetEvidence(
		hit,
		"headings",
		doc.Headings,
		allowedTerms,
		req.Fuzzy,
	); found {
		return locationBiasedSnippet(evidence, fallback)
	}
	if evidence, found := fieldSnippetEvidence(
		hit,
		"anchors",
		anchorTexts(doc.Inlinks),
		allowedTerms,
		req.Fuzzy,
	); found {
		return locationBiasedSnippet(evidence, fallback)
	}

	return queryBiasedSnippet(doc.ExtractedText, terms, fallback)
}

func locationBiasedSnippet(evidence snippetEvidence, fallback string) string {
	text := evidence.text
	anchor := evidence.start
	start := anchor
	for index := 0; index < snippetRuneCap/4 && start > 0; index++ {
		_, size := utf8.DecodeLastRuneInString(text[:start])
		start -= size
	}
	end := start
	windowRunes := 0
	for windowRunes < snippetRuneCap && end < len(text) {
		_, size := utf8.DecodeRuneInString(text[end:])
		end += size
		windowRunes++
	}
	for windowRunes < snippetRuneCap && start > 0 {
		_, size := utf8.DecodeLastRuneInString(text[:start])
		start -= size
		windowRunes++
	}
	excerpt := strings.Join(strings.Fields(text[start:end]), " ")
	if excerpt == "" {
		return fallback
	}
	if start > 0 {
		excerpt = "… " + excerpt
	}

	return excerpt
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

func fieldSnippetEvidence(
	hit *search.DocumentMatch,
	field string,
	values []string,
	allowedTerms map[string]struct{},
	acceptAnyTerm bool,
) (snippetEvidence, bool) {
	var selected snippetEvidence
	found := false
	for term, locations := range hit.Locations[field] {
		if _, allowed := allowedTerms[term]; !acceptAnyTerm && !allowed {
			continue
		}
		for _, location := range locations {
			candidate, valid := locationSnippetEvidence(values, location)
			if !valid {
				continue
			}
			if !found || candidate.arrayIndex < selected.arrayIndex ||
				(candidate.arrayIndex == selected.arrayIndex && candidate.start < selected.start) {
				selected = candidate
				found = true
			}
			break
		}
	}

	return selected, found
}

func locationSnippetEvidence(
	values []string,
	location *search.Location,
) (snippetEvidence, bool) {
	if location == nil || len(values) == 0 || location.End <= location.Start {
		return snippetEvidence{}, false
	}
	arrayIndex := uint64(0)
	if len(location.ArrayPositions) > 0 {
		arrayIndex = location.ArrayPositions[0]
	}
	if arrayIndex >= uint64(len(values)) {
		return snippetEvidence{}, false
	}
	value := values[arrayIndex]
	if location.End > uint64(len(value)) {
		return snippetEvidence{}, false
	}
	term := value[location.Start:location.End]
	if term == "" || !utf8.ValidString(term) {
		return snippetEvidence{}, false
	}

	return snippetEvidence{
		text:       value,
		arrayIndex: arrayIndex,
		start:      len(value[:location.Start]),
	}, true
}
