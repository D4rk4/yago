package searchindex

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2/search"
)

func scanVisibleFieldEvidence(
	ctx context.Context,
	matcher *storedEvidenceMatcher,
	text string,
	includeMatches bool,
) (map[string][]int, []TextQueryMatch, error) {
	if isCJKAnalyzer(matcher.name) {
		return scanVisibleCJKFieldEvidence(ctx, matcher, text, includeMatches)
	}
	positions := emptyVisibleFieldPositions(matcher)
	matches := []TextQueryMatch{}
	assignment := storedFieldEvidence{latestRaw: map[int]*search.Location{}}
	position := 0
	for start, end := range rangeStoredTokens(text) {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("visible text evidence: %w", err)
		}
		position++
		token := text[start:end]
		target, found := assignment.selectRawRequirement(
			matcher,
			matcher.match(token).targets,
			token,
		)
		if !found {
			continue
		}
		location := &search.Location{
			Pos:   storedLocationCoordinate(position),
			Start: storedLocationCoordinate(start),
			End:   storedLocationCoordinate(end),
		}
		assignment.latestRaw[target.rawRequirement] = location
		positions[target.raw] = appendVisiblePosition(positions[target.raw], position)
		if includeMatches && len(matches) < maximumAnalyzedQueryMatches {
			matches = append(matches, TextQueryMatch{Start: start, End: end})
		}
	}

	return positions, matches, nil
}

func scanVisibleCJKFieldEvidence(
	ctx context.Context,
	matcher *storedEvidenceMatcher,
	text string,
	includeMatches bool,
) (map[string][]int, []TextQueryMatch, error) {
	evidence, err := scanStoredCJKFieldEvidence(ctx, matcher, []string{text}, true)
	if err != nil {
		return nil, nil, err
	}
	request := SearchRequest{Terms: matcher.rawRequirements, IncludePositions: true}
	positions := boundedFieldTermPositions(request, evidence.requirementTerms)
	matches := []TextQueryMatch{}
	if includeMatches {
		matches = boundedBodyQueryMatches(text, evidence.requirementTerms)
	}

	return positions, matches, nil
}

func emptyVisibleFieldPositions(matcher *storedEvidenceMatcher) map[string][]int {
	positions := make(map[string][]int, len(matcher.rawRequirements))
	for _, requirement := range matcher.rawRequirements {
		positions[requirement] = nil
	}

	return positions
}

func appendVisiblePosition(positions []int, position int) []int {
	if len(positions) < maximumTermPositionsPerField {
		return append(positions, position)
	}
	positions[len(positions)-1] = position

	return positions
}
