package searchremote

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchvisible"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	maximumAppliedEvidenceFields       = 5
	maximumAppliedEvidenceRequirements = 32
	maximumAppliedEvidencePositions    = 256
	maximumAppliedRequirementPositions = 64
	maximumAppliedEvidenceMatches      = 128
	maximumAppliedEvidenceSnippetBytes = 2 << 10
	maximumAppliedEvidenceOffset       = 1 << 30
)

func resultWithQueryMatchEvidence(
	requirements []string,
	result searchcore.Result,
	evidence map[yagomodel.Hash]yagoproto.QueryMatchEvidence,
) searchcore.Result {
	return resultWithQueryMatchEvidenceBinding(
		identityQueryMatchEvidenceBinding(requirements),
		result,
		evidence,
	)
}

func resultWithQueryMatchEvidenceBinding(
	binding queryMatchEvidenceBinding,
	result searchcore.Result,
	evidence map[yagomodel.Hash]yagoproto.QueryMatchEvidence,
) searchcore.Result {
	if !binding.valid() {
		return result
	}
	resource := yagomodel.Hash(result.URLHash)
	if !binding.allowsResource(resource) {
		return result
	}
	item, found := evidence[resource]
	if !found || !validAppliedQueryMatchEvidence(item, binding.wireRequirements, result) {
		return result
	}
	positions := coreFieldRequirementPositions(item.FieldPositions, binding.rankingRequirements)
	result.Analyzer = item.Analyzer
	result.EvidenceReady = true
	result.EvidenceRequirementOrdinals = slices.Clone(item.RequirementOrdinals)
	result.FieldTermPositions = positions
	result.BodyQueryMatches = coreProtocolMatches(item.BodyMatches)
	result.QueryMatches = []searchcore.QueryMatch{}
	if item.Snippet != "" {
		result.Snippet = item.Snippet
		result.QueryMatches = coreProtocolMatches(item.SnippetMatches)
	}

	return result
}

func validAppliedQueryMatchEvidence(
	evidence yagoproto.QueryMatchEvidence,
	requirements []string,
	result searchcore.Result,
) bool {
	if !validAppliedEvidenceHeader(evidence) ||
		!validAppliedEvidenceRequirements(requirements) ||
		!validAppliedEvidenceCompatibility(evidence, result) {
		return false
	}

	return validAppliedEvidenceFields(evidence.FieldPositions, len(requirements)) &&
		validAppliedEvidenceCompleteness(evidence, requirements)
}

func validAppliedEvidenceCompleteness(
	evidence yagoproto.QueryMatchEvidence,
	requirements []string,
) bool {
	expected, available := searchvisible.AnalyzerRequirementOrdinals(
		evidence.Analyzer,
		requirements,
	)
	if !available || evidence.RequirementOrdinals == nil || evidence.AbsentOrdinals == nil ||
		!slices.Equal(evidence.RequirementOrdinals, expected) ||
		len(evidence.AbsentOrdinals) > len(expected) ||
		!validAppliedEvidenceOrdinals(evidence.AbsentOrdinals, len(requirements)) {
		return false
	}
	relevant := make(map[int]struct{}, len(expected))
	absent := make(map[int]struct{}, len(evidence.AbsentOrdinals))
	present := make(map[int]struct{}, len(expected)-len(evidence.AbsentOrdinals))
	for _, ordinal := range expected {
		relevant[ordinal] = struct{}{}
	}
	for _, ordinal := range evidence.AbsentOrdinals {
		if _, found := relevant[ordinal]; !found {
			return false
		}
		absent[ordinal] = struct{}{}
	}
	for _, field := range evidence.FieldPositions {
		for _, requirement := range field.Requirements {
			if _, found := relevant[requirement.Ordinal]; !found {
				return false
			}
			if _, found := absent[requirement.Ordinal]; found {
				return false
			}
			present[requirement.Ordinal] = struct{}{}
		}
	}

	return len(present)+len(absent) == len(relevant)
}

func validAppliedEvidenceOrdinals(ordinals []int, requirementTotal int) bool {
	previous := -1
	for _, ordinal := range ordinals {
		if ordinal <= previous || ordinal >= requirementTotal {
			return false
		}
		previous = ordinal
	}

	return true
}

func validAppliedEvidenceHeader(evidence yagoproto.QueryMatchEvidence) bool {
	return evidence.Version == yagoproto.QueryMatchEvidenceVersion &&
		searchvisible.AnalyzerAvailable(evidence.Analyzer) &&
		len(evidence.Snippet) <= maximumAppliedEvidenceSnippetBytes &&
		utf8.ValidString(evidence.Snippet) &&
		len(evidence.SnippetMatches) <= maximumAppliedEvidenceMatches &&
		len(evidence.BodyMatches) <= maximumAppliedEvidenceMatches &&
		len(evidence.FieldPositions) <= maximumAppliedEvidenceFields &&
		validAppliedMatchRanges(
			evidence.SnippetMatches,
			len(evidence.Snippet),
			evidence.Snippet,
		) &&
		validAppliedMatchRanges(evidence.BodyMatches, maximumAppliedEvidenceOffset, "")
}

func validAppliedEvidenceRequirements(requirements []string) bool {
	if len(requirements) == 0 || len(requirements) > maximumAppliedEvidenceRequirements {
		return false
	}
	for _, requirement := range requirements {
		if strings.TrimSpace(requirement) == "" {
			return false
		}
	}

	return true
}

func validAppliedEvidenceCompatibility(
	evidence yagoproto.QueryMatchEvidence,
	result searchcore.Result,
) bool {
	visibleSnippet := result.Snippet
	if evidence.Snippet != "" {
		visibleSnippet = evidence.Snippet
	}

	return searchvisible.AnalyzerCompatible(
		evidence.Analyzer,
		result.Language,
		searchvisible.Text{
			Title:   result.Title,
			Snippet: visibleSnippet,
			URL:     result.URL,
		},
	)
}

func validAppliedEvidenceFields(
	fieldPositions []yagoproto.QueryFieldPositions,
	requirementTotal int,
) bool {
	fields := make(map[string]struct{}, len(fieldPositions))
	positions := 0
	for _, field := range fieldPositions {
		if !appliedEvidenceField(field.Field) ||
			len(field.Requirements) > maximumAppliedEvidenceRequirements {
			return false
		}
		if _, duplicate := fields[field.Field]; duplicate {
			return false
		}
		fields[field.Field] = struct{}{}
		fieldPositions, valid := validAppliedFieldPositions(field, requirementTotal)
		if !valid || positions+fieldPositions > maximumAppliedEvidencePositions {
			return false
		}
		positions += fieldPositions
	}

	return true
}

func validAppliedFieldPositions(
	field yagoproto.QueryFieldPositions,
	requirementTotal int,
) (int, bool) {
	ordinals := make(map[int]struct{}, len(field.Requirements))
	positions := 0
	for _, requirement := range field.Requirements {
		if requirement.Ordinal < 0 || requirement.Ordinal >= requirementTotal ||
			len(requirement.Positions) == 0 ||
			len(requirement.Positions) > maximumAppliedRequirementPositions ||
			!validAppliedPositions(requirement.Positions) {
			return 0, false
		}
		if _, duplicate := ordinals[requirement.Ordinal]; duplicate {
			return 0, false
		}
		ordinals[requirement.Ordinal] = struct{}{}
		positions += len(requirement.Positions)
	}

	return positions, true
}

func appliedEvidenceField(field string) bool {
	switch field {
	case "title", "headings", "anchors", "body", "url":
		return true
	default:
		return false
	}
}

func validAppliedPositions(positions []int) bool {
	previous := 0
	for _, position := range positions {
		if position <= previous || position > maximumAppliedEvidenceOffset {
			return false
		}
		previous = position
	}

	return true
}

func validAppliedMatchRanges(
	matches []yagoproto.QueryMatchRange,
	boundary int,
	text string,
) bool {
	previous := yagoproto.QueryMatchRange{}
	for index, match := range matches {
		if match.Start < 0 || match.End <= match.Start || match.End > boundary ||
			index > 0 && (match.Start < previous.Start ||
				match.Start == previous.Start && match.End <= previous.End) {
			return false
		}
		if text != "" && (!utf8.RuneStart(text[match.Start]) ||
			match.End < len(text) && !utf8.RuneStart(text[match.End])) {
			return false
		}
		previous = match
	}

	return true
}

func coreFieldRequirementPositions(
	fields []yagoproto.QueryFieldPositions,
	requirements []string,
) map[string]map[string][]int {
	mapped := make(map[string]map[string][]int, len(fields))
	for _, field := range fields {
		terms := make(map[string][]int, len(field.Requirements))
		for _, requirement := range field.Requirements {
			term := strings.ToLower(strings.TrimSpace(requirements[requirement.Ordinal]))
			terms[term] = slices.Clone(requirement.Positions)
		}
		mapped[field.Field] = terms
	}

	return mapped
}

func coreProtocolMatches(matches []yagoproto.QueryMatchRange) []searchcore.QueryMatch {
	mapped := make([]searchcore.QueryMatch, len(matches))
	for index, match := range matches {
		mapped[index] = searchcore.QueryMatch{Start: match.Start, End: match.End}
	}

	return mapped
}
