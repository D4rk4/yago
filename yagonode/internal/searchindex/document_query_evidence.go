package searchindex

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const MaximumDocumentQueryEvidenceBytes = 512 << 10

type DocumentQueryEvidence struct {
	Analyzer                  string
	RequirementOrdinals       []int
	AbsentOrdinals            []int
	Snippet                   string
	SnippetMatches            []TextQueryMatch
	BodyMatches               []TextQueryMatch
	FieldRequirementPositions map[string]map[int][]int
}

func AnalyzeDocumentQueryEvidence(
	ctx context.Context,
	document documentstore.Document,
	requirements []string,
	byteLimit int,
) (DocumentQueryEvidence, int, bool, error) {
	if err := ctx.Err(); err != nil {
		return DocumentQueryEvidence{}, 0, false, fmt.Errorf(
			"analyze document query evidence context: %w",
			err,
		)
	}
	if len(requirements) == 0 || loadStemmingMapping() == nil || byteLimit <= 0 {
		return DocumentQueryEvidence{}, 0, false, nil
	}
	document, analyzedBytes, valid := boundedEvidenceDocument(
		document,
		min(byteLimit, MaximumDocumentQueryEvidenceBytes),
	)
	if !valid {
		return DocumentQueryEvidence{}, analyzedBytes, false, nil
	}
	analyzer := detectDocumentAnalyzer(analyzerDetectionText(document), document.Language)
	request := SearchRequest{Terms: requirements, IncludePositions: true}
	stored, err := storedDocumentLocations(ctx, document, request, analyzer)
	if err != nil {
		return DocumentQueryEvidence{}, analyzedBytes, false, fmt.Errorf(
			"analyze document query evidence: %w",
			err,
		)
	}
	matcher := newStoredEvidenceMatcher(request, analyzer)
	fields := documentRequirementPositions(request, matcher, stored)
	urlPositions, _, err := scanVisibleFieldEvidence(
		ctx,
		matcher,
		document.NormalizedURL,
		false,
	)
	if err != nil {
		return DocumentQueryEvidence{}, analyzedBytes, false, fmt.Errorf(
			"analyze document URL evidence: %w",
			err,
		)
	}
	fields["url"] = ordinalRequirementPositions(matcher, urlPositions)
	snippet := queryEvidenceSnippet(document, requirements, stored.bodyQueryMatches)
	_, snippetMatches, err := scanVisibleFieldEvidence(ctx, matcher, snippet, true)
	if err != nil {
		return DocumentQueryEvidence{}, analyzedBytes, false, fmt.Errorf(
			"analyze document snippet evidence: %w",
			err,
		)
	}

	return DocumentQueryEvidence{
		Analyzer:            matcher.name,
		RequirementOrdinals: append([]int{}, matcher.rawRequirementOrdinals...),
		AbsentOrdinals: absentDocumentRequirementOrdinals(
			matcher.rawRequirementOrdinals,
			fields,
		),
		Snippet:                   snippet,
		SnippetMatches:            snippetMatches,
		BodyMatches:               stored.bodyQueryMatches,
		FieldRequirementPositions: fields,
	}, analyzedBytes, true, nil
}

func StoredEvidenceAnalyzerAvailable(name string) bool {
	mapping := loadStemmingMapping()
	return mapping != nil && mapping.AnalyzerNamed(name) != nil
}

func StoredEvidenceAnalyzerCompatible(
	name string,
	language string,
	visible VisibleText,
) bool {
	if !StoredEvidenceAnalyzerAvailable(name) {
		return false
	}
	bounded, valid := boundedVisibleText(visible)
	if !valid {
		return false
	}
	detectionText := bounded.Snippet
	if len(bounded.Title) > len(detectionText) {
		detectionText = bounded.Title
	}
	if detectionText == "" {
		detectionText = bounded.URL
	}
	if name == visibleTextAnalyzer(detectionText, language) {
		return true
	}

	return slices.Contains(scriptAnalyzers(dominantScript(detectionText)), name)
}

func documentRequirementPositions(
	request SearchRequest,
	matcher *storedEvidenceMatcher,
	evidence storedDocumentEvidence,
) map[string]map[int][]int {
	fields := make(map[string]map[int][]int, len(evidence.locations)+1)
	for field, terms := range evidence.locations {
		requirementTerms := make(search.TermLocationMap, len(matcher.rawRequirements))
		for _, requirement := range matcher.rawRequirements {
			requirementTerms[requirement] = evidenceRequirementLocations(
				requirement,
				matcher,
				terms,
			)
		}
		fields[field] = ordinalRequirementPositions(
			matcher,
			boundedFieldTermPositions(request, requirementTerms),
		)
	}

	return fields
}

func evidenceRequirementLocations(
	requirement string,
	matcher *storedEvidenceMatcher,
	terms search.TermLocationMap,
) search.Locations {
	locations := search.Locations{}
	for _, analyzed := range matcher.rawRequirementAnalyzedTerms[requirement] {
		locations = append(locations, terms[analyzed]...)
	}
	sort.Slice(locations, func(left int, right int) bool {
		return locations[left].Pos < locations[right].Pos
	})

	return locations
}

func ordinalRequirementPositions(
	matcher *storedEvidenceMatcher,
	positions map[string][]int,
) map[int][]int {
	ordinals := make(map[int][]int, len(positions))
	for index, requirement := range matcher.rawRequirements {
		if values := positions[requirement]; len(values) > 0 {
			ordinals[matcher.rawRequirementOrdinals[index]] = values
		}
	}

	return ordinals
}

func queryEvidenceSnippet(
	document documentstore.Document,
	requirements []string,
	matches []TextQueryMatch,
) string {
	fallback := documentTitle(document)
	if len(matches) == 0 {
		return queryBiasedSnippet(document.ExtractedText, requirements, fallback)
	}
	match := matches[0]
	if match.Start < 0 || match.End > len(document.ExtractedText) || match.End <= match.Start {
		return queryBiasedSnippet(document.ExtractedText, requirements, fallback)
	}

	return queryBiasedSnippetWithEvidence(
		document.ExtractedText,
		requirements,
		document.ExtractedText[match.Start:match.End],
		fallback,
	)
}

func boundedEvidenceDocument(
	document documentstore.Document,
	byteLimit int,
) (documentstore.Document, int, bool) {
	document.Headings = slices.Clone(document.Headings)
	document.Inlinks = slices.Clone(document.Inlinks)
	remaining := byteLimit
	valid := true
	document.Title, remaining, valid = boundedEvidenceText(document.Title, remaining, valid)
	document.NormalizedURL, remaining, valid = boundedEvidenceText(
		document.NormalizedURL,
		remaining,
		valid,
	)
	for index := range document.Headings {
		document.Headings[index], remaining, valid = boundedEvidenceText(
			document.Headings[index],
			remaining,
			valid,
		)
	}
	for index := range document.Inlinks {
		document.Inlinks[index].Text, remaining, valid = boundedEvidenceText(
			document.Inlinks[index].Text,
			remaining,
			valid,
		)
	}
	document.ExtractedText, remaining, valid = boundedEvidenceText(
		document.ExtractedText,
		remaining,
		valid,
	)

	return document, byteLimit - remaining, valid
}

func boundedEvidenceText(text string, remaining int, valid bool) (string, int, bool) {
	if !valid || !utf8.ValidString(text) {
		return "", remaining, false
	}
	if remaining <= 0 {
		return "", 0, true
	}
	if len(text) <= remaining {
		return text, remaining - len(text), true
	}
	end := remaining
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}

	return strings.TrimSpace(text[:end]), 0, true
}
