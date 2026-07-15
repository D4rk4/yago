package searchindex

import (
	"context"
	"sort"

	"github.com/blevesearch/bleve/v2/search"
)

type TextQueryEvidence struct {
	Start int
	End   int
}

type textQueryLocation struct {
	requirement int
	start       int
	end         int
}

func FindTextQueryEvidence(
	ctx context.Context,
	text string,
	terms []string,
	language string,
) (TextQueryEvidence, bool) {
	if text == "" || len(terms) == 0 || ctx.Err() != nil {
		return TextQueryEvidence{}, false
	}
	analyzers := []string{detectDocumentAnalyzer(text, "")}
	if hinted, found := analyzerFromLangHint(language); found {
		analyzers = append(analyzers, hinted)
	}
	analyzers = append(analyzers, standardTextAnalyzer)
	seen := map[string]struct{}{}
	for _, analyzer := range analyzers {
		if _, exists := seen[analyzer]; exists {
			continue
		}
		seen[analyzer] = struct{}{}
		matcher := newStoredEvidenceMatcher(SearchRequest{Terms: terms}, analyzer)
		field, err := scanStoredFieldEvidence(ctx, matcher, []string{text}, true)
		if err != nil {
			return TextQueryEvidence{}, false
		}
		if evidence, found := minimumTargetTextQueryEvidence(
			text,
			matcher,
			field.targetTerms,
		); found {
			return evidence, true
		}
	}

	return TextQueryEvidence{}, false
}

func minimumTargetTextQueryEvidence(
	text string,
	matcher *storedEvidenceMatcher,
	locations map[int]search.Locations,
) (TextQueryEvidence, bool) {
	flat := make([]textQueryLocation, 0, len(matcher.targets))
	for requirement := range matcher.targets {
		targetLocations := locations[requirement]
		if len(targetLocations) == 0 {
			return TextQueryEvidence{}, false
		}
		valid := 0
		for _, location := range targetLocations {
			if location == nil || location.End <= location.Start ||
				location.End > uint64(len(text)) {
				continue
			}
			flat = append(flat, textQueryLocation{
				requirement: requirement,
				start:       len(text[:location.Start]),
				end:         len(text[:location.End]),
			})
			valid++
		}
		if valid == 0 {
			return TextQueryEvidence{}, false
		}
	}
	sort.Slice(flat, func(left int, right int) bool {
		if flat[left].start != flat[right].start {
			return flat[left].start < flat[right].start
		}

		return flat[left].end < flat[right].end
	})

	return tightestTextQueryEvidence(flat, len(matcher.targets), len(text))
}

func minimumTextQueryEvidence(
	text string,
	matcher *storedEvidenceMatcher,
	locations search.TermLocationMap,
) (TextQueryEvidence, bool) {
	flat, complete := flattenTextQueryLocations(text, matcher, locations)
	if !complete {
		return TextQueryEvidence{}, false
	}
	sort.Slice(flat, func(left int, right int) bool {
		if flat[left].start != flat[right].start {
			return flat[left].start < flat[right].start
		}

		return flat[left].end < flat[right].end
	})

	return tightestTextQueryEvidence(flat, len(matcher.required), len(text))
}

func flattenTextQueryLocations(
	text string,
	matcher *storedEvidenceMatcher,
	locations search.TermLocationMap,
) ([]textQueryLocation, bool) {
	flat := make([]textQueryLocation, 0, len(matcher.required))
	for requirement, term := range matcher.required {
		termLocations := locations[term]
		if len(termLocations) == 0 {
			return nil, false
		}
		valid := 0
		for _, location := range termLocations {
			if location == nil || location.End <= location.Start ||
				location.End > uint64(len(text)) {
				continue
			}
			flat = append(flat, textQueryLocation{
				requirement: requirement,
				start:       len(text[:location.Start]),
				end:         len(text[:location.End]),
			})
			valid++
		}
		if valid == 0 {
			return nil, false
		}
	}

	return flat, true
}

func tightestTextQueryEvidence(
	flat []textQueryLocation,
	requirements int,
	textLength int,
) (TextQueryEvidence, bool) {
	counts := make([]int, requirements)
	covered := 0
	left := 0
	best := TextQueryEvidence{}
	bestSpan := textLength + 1
	maximumEnds := make([]int, 0, len(flat))
	for right, location := range flat {
		if counts[location.requirement] == 0 {
			covered++
		}
		counts[location.requirement]++
		for len(maximumEnds) > 0 && flat[maximumEnds[len(maximumEnds)-1]].end <= location.end {
			maximumEnds = maximumEnds[:len(maximumEnds)-1]
		}
		maximumEnds = append(maximumEnds, right)
		for covered == requirements {
			end := flat[maximumEnds[0]].end
			if span := end - flat[left].start; span < bestSpan {
				bestSpan = span
				best = TextQueryEvidence{Start: flat[left].start, End: end}
			}
			if maximumEnds[0] == left {
				maximumEnds = maximumEnds[1:]
			}
			leftRequirement := flat[left].requirement
			counts[leftRequirement]--
			if counts[leftRequirement] == 0 {
				covered--
			}
			left++
		}
	}

	return best, bestSpan <= textLength
}
