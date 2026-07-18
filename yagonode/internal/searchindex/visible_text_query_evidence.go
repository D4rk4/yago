package searchindex

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	maximumVisibleTextQueryRequirements = 32
	maximumVisibleTitleBytes            = 4 << 10
	maximumVisibleSnippetBytes          = 16 << 10
	maximumVisibleURLBytes              = 8 << 10
)

type VisibleText struct {
	Title   string
	Snippet string
	URL     string
}

type VisibleTextQueryEvidence struct {
	Analyzer                    string
	EvidenceRequirementOrdinals []int
	FieldTermPositions          map[string]map[string][]int
	QueryMatches                []TextQueryMatch
}

type VisibleTextQuery struct {
	requirements []string
	matchers     map[string]*storedEvidenceMatcher
}

func NewVisibleTextQuery(terms []string) (*VisibleTextQuery, bool) {
	if len(terms) == 0 || loadStemmingMapping() == nil {
		return nil, false
	}
	requirements := boundedVisibleTextRequirements(terms)
	if len(requirements) == 0 {
		return nil, false
	}

	return &VisibleTextQuery{
		requirements: requirements,
		matchers:     make(map[string]*storedEvidenceMatcher),
	}, true
}

func AnalyzeVisibleTextQueryEvidence(
	ctx context.Context,
	terms []string,
	language string,
	visible VisibleText,
) (VisibleTextQueryEvidence, bool, error) {
	if err := ctx.Err(); err != nil {
		return VisibleTextQueryEvidence{}, false, fmt.Errorf(
			"analyze visible text query context: %w",
			err,
		)
	}
	query, available := NewVisibleTextQuery(terms)
	if !available {
		return VisibleTextQueryEvidence{}, false, nil
	}

	return query.Analyze(ctx, language, visible)
}

func (q *VisibleTextQuery) Analyze(
	ctx context.Context,
	language string,
	visible VisibleText,
) (VisibleTextQueryEvidence, bool, error) {
	if err := ctx.Err(); err != nil {
		return VisibleTextQueryEvidence{}, false, fmt.Errorf(
			"analyze visible text context: %w",
			err,
		)
	}
	bounded, valid := boundedVisibleText(visible)
	if !valid {
		return VisibleTextQueryEvidence{}, false, nil
	}
	detectionText := bounded.Snippet
	if len(bounded.Title) > len(detectionText) {
		detectionText = bounded.Title
	}
	if detectionText == "" {
		detectionText = bounded.URL
	}
	analyzer := visibleTextAnalyzer(detectionText, language)
	request := SearchRequest{Terms: q.requirements, IncludePositions: true}
	matcher := q.matchers[analyzer]
	if matcher == nil {
		matcher = newStoredEvidenceMatcher(request, analyzer)
		matcher.normalizeCandidateAnchors = true
		q.matchers[analyzer] = matcher
	}
	fields := []struct {
		name string
		text string
	}{
		{name: "title", text: bounded.Title},
		{name: "snippet", text: bounded.Snippet},
		{name: "url", text: bounded.URL},
	}
	positions := make(map[string]map[string][]int, len(fields))
	matches := []TextQueryMatch{}
	for _, field := range fields {
		if field.text == "" {
			continue
		}
		fieldPositions, fieldMatches, err := scanVisibleFieldEvidence(
			ctx,
			matcher,
			field.text,
			field.name == "snippet",
		)
		if err != nil {
			return VisibleTextQueryEvidence{}, false, err
		}
		positions[field.name] = fieldPositions
		if field.name == "snippet" {
			matches = fieldMatches
		}
	}

	return VisibleTextQueryEvidence{
		Analyzer:                    analyzer,
		EvidenceRequirementOrdinals: append([]int{}, matcher.rawRequirementOrdinals...),
		FieldTermPositions:          positions,
		QueryMatches:                matches,
	}, true, nil
}

func visibleTextAnalyzer(text string, language string) string {
	if analyzer, found := scriptQualifiedLanguageAnalyzer(language, text); found {
		return analyzer
	}
	script := dominantScript(text)
	candidates := scriptAnalyzers(script)
	if hinted, found := analyzerFromLangHint(language); found &&
		(script == nil || slices.Contains(candidates, hinted)) {
		return hinted
	}
	if len(candidates) > 0 {
		return candidates[0]
	}

	return standardTextAnalyzer
}

func boundedVisibleText(visible VisibleText) (VisibleText, bool) {
	if !utf8.ValidString(visible.Title) || !utf8.ValidString(visible.Snippet) ||
		!utf8.ValidString(visible.URL) {
		return VisibleText{}, false
	}
	visible.Title = boundedVisibleTextField(visible.Title, maximumVisibleTitleBytes)
	visible.Snippet = boundedVisibleTextField(visible.Snippet, maximumVisibleSnippetBytes)
	visible.URL = boundedVisibleTextField(visible.URL, maximumVisibleURLBytes)
	if visible.Title == "" && visible.Snippet == "" && visible.URL == "" {
		return VisibleText{}, false
	}

	return visible, true
}

func boundedVisibleTextField(text string, maximumBytes int) string {
	if len(text) <= maximumBytes {
		return text
	}
	end := maximumBytes
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}

	return text[:end]
}

func boundedVisibleTextRequirements(terms []string) []string {
	requirements := make([]string, 0, min(len(terms), maximumVisibleTextQueryRequirements))
	for _, term := range terms[:min(len(terms), maximumVisibleTextQueryRequirements)] {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		requirements = append(requirements, term)
	}

	return requirements
}
