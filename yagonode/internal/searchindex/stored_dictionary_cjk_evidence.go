package searchindex

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2/search"
)

func scanStoredDictionaryCJKFieldEvidence(
	ctx context.Context,
	matcher *storedEvidenceMatcher,
	values []string,
	includePositions bool,
) (storedFieldEvidence, error) {
	if matcher.analyzer == nil {
		fallback := *matcher
		fallback.name = "cjk"

		return scanStoredCJKFieldEvidence(ctx, &fallback, values, includePositions)
	}
	field := newStoredFieldEvidence(matcher)
	positionBase := 0
	for arrayIndex, value := range values {
		field.latest = map[int]*search.Location{}
		field.latestRaw = map[int]*search.Location{}
		field.exactLatest = map[int]*search.Location{}
		maximumPosition := 0
		for _, token := range matcher.analyzer.Analyze([]byte(value)) {
			if err := ctx.Err(); err != nil {
				return storedFieldEvidence{}, fmt.Errorf("stored search evidence: %w", err)
			}
			if token.Start < 0 || token.End <= token.Start || token.End > len(value) {
				continue
			}
			maximumPosition = max(maximumPosition, token.Position)
			matched := matcher.lookup[string(token.Term)]
			if len(matched) == 0 {
				continue
			}
			location := newStoredLocation(storedLocationCoordinates{
				position:    storedLocationCoordinate(positionBase + token.Position),
				start:       token.Start,
				end:         token.End,
				arrayIndex:  arrayIndex,
				arrayLength: len(values),
			})
			field.addMatches(matcher, matched, location, value[token.Start:token.End])
			field.observeWindow()
			matcher.observeRelaxedPassage(field.exactLatest)
			if !includePositions && len(field.latest) == field.queryTerms &&
				(matcher.minimumPassage == 0 || matcher.relaxedPassageEvidence) {
				break
			}
		}
		positionBase += maximumPosition + 1
	}
	collapseStoredCJKRequirements(matcher, values, &field)
	field.preserveWitnesses(matcher)
	if matcher.quotedPhrases.enabled() {
		field.phraseTerms = field.terms
	}

	return field, nil
}
