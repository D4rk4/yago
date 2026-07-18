package yagoproto

import "unicode/utf8"

func validQueryMatchEvidenceEnvelope(evidence QueryMatchEvidence) bool {
	return evidence.Version == QueryMatchEvidenceVersion &&
		validResourceAnalyzer(evidence.Analyzer) &&
		validQueryMatchEvidenceCompleteness(evidence) &&
		len(evidence.Snippet) <= maximumResourceSnippetBytes &&
		utf8.ValidString(evidence.Snippet) &&
		len(evidence.SnippetMatches) <= maximumResourceMatches &&
		len(evidence.BodyMatches) <= maximumResourceMatches &&
		len(evidence.FieldPositions) <= maximumResourceEvidenceFields
}

func validQueryMatchEvidenceCompleteness(evidence QueryMatchEvidence) bool {
	if evidence.RequirementOrdinals == nil || evidence.AbsentOrdinals == nil ||
		len(evidence.RequirementOrdinals) > maximumResourceRequirements ||
		len(evidence.AbsentOrdinals) > len(evidence.RequirementOrdinals) ||
		!validQueryMatchEvidenceOrdinals(evidence.RequirementOrdinals) ||
		!validQueryMatchEvidenceOrdinals(evidence.AbsentOrdinals) {
		return false
	}
	relevant := make(map[int]struct{}, len(evidence.RequirementOrdinals))
	for _, ordinal := range evidence.RequirementOrdinals {
		relevant[ordinal] = struct{}{}
	}
	for _, ordinal := range evidence.AbsentOrdinals {
		if _, found := relevant[ordinal]; !found {
			return false
		}
	}

	return true
}

func validQueryMatchEvidenceOrdinals(ordinals []int) bool {
	previous := -1
	for _, ordinal := range ordinals {
		if ordinal <= previous || ordinal >= maximumResourceRequirements {
			return false
		}
		previous = ordinal
	}

	return true
}

func validQueryMatchEvidenceFields(fields []QueryFieldPositions) bool {
	uniqueFields := make(map[string]struct{}, len(fields))
	totalPositions := 0
	for _, field := range fields {
		if _, found := uniqueFields[field.Field]; found {
			return false
		}
		positions, valid := validQueryMatchEvidenceField(field)
		if !valid {
			return false
		}
		uniqueFields[field.Field] = struct{}{}
		totalPositions += positions
		if totalPositions > maximumResourcePositions {
			return false
		}
	}

	return true
}

func validQueryMatchEvidencePartition(evidence QueryMatchEvidence) bool {
	relevant := make(map[int]struct{}, len(evidence.RequirementOrdinals))
	accounted := make(map[int]struct{}, len(evidence.RequirementOrdinals))
	absent := make(map[int]struct{}, len(evidence.AbsentOrdinals))
	for _, ordinal := range evidence.RequirementOrdinals {
		relevant[ordinal] = struct{}{}
	}
	for _, ordinal := range evidence.AbsentOrdinals {
		accounted[ordinal] = struct{}{}
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
			accounted[requirement.Ordinal] = struct{}{}
		}
	}

	return len(accounted) == len(relevant)
}

func validQueryMatchEvidenceField(field QueryFieldPositions) (int, bool) {
	if !validEvidenceField(field.Field) ||
		len(field.Requirements) > maximumResourceRequirements {
		return 0, false
	}
	uniqueRequirements := make(map[int]struct{}, len(field.Requirements))
	positions := 0
	for _, requirement := range field.Requirements {
		if !validQueryMatchEvidenceRequirement(requirement) {
			return 0, false
		}
		if _, found := uniqueRequirements[requirement.Ordinal]; found {
			return 0, false
		}
		uniqueRequirements[requirement.Ordinal] = struct{}{}
		positions += len(requirement.Positions)
	}

	return positions, true
}

func validQueryMatchEvidenceRequirement(requirement QueryRequirementPositions) bool {
	return requirement.Ordinal >= 0 &&
		requirement.Ordinal < maximumResourceRequirements &&
		len(requirement.Positions) > 0 &&
		len(requirement.Positions) <= maximumRequirementPositions &&
		validEvidencePositions(requirement.Positions)
}
