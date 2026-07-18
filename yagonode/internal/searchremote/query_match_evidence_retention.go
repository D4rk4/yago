package searchremote

import (
	"slices"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func retainedQueryMatchEvidence(
	evidence map[yagomodel.Hash]yagoproto.QueryMatchEvidence,
	resources []yagomodel.URIMetadataRow,
) map[yagomodel.Hash]yagoproto.QueryMatchEvidence {
	if len(evidence) == 0 {
		return nil
	}
	retained := make(map[yagomodel.Hash]yagoproto.QueryMatchEvidence, len(resources))
	for _, resource := range resources {
		hash, err := resource.URLHash()
		if err != nil {
			continue
		}
		item, found := evidence[yagomodel.Hash(hash)]
		if !found {
			continue
		}
		item.Snippet = strings.Clone(item.Snippet)
		item.RequirementOrdinals = slices.Clone(item.RequirementOrdinals)
		item.AbsentOrdinals = slices.Clone(item.AbsentOrdinals)
		item.SnippetMatches = slices.Clone(item.SnippetMatches)
		item.BodyMatches = slices.Clone(item.BodyMatches)
		item.FieldPositions = cloneQueryFieldPositions(item.FieldPositions)
		retained[yagomodel.Hash(hash)] = item
	}
	if len(retained) == 0 {
		return nil
	}

	return retained
}

func cloneQueryFieldPositions(
	fields []yagoproto.QueryFieldPositions,
) []yagoproto.QueryFieldPositions {
	cloned := make([]yagoproto.QueryFieldPositions, len(fields))
	for fieldIndex, field := range fields {
		cloned[fieldIndex] = field
		cloned[fieldIndex].Requirements = make(
			[]yagoproto.QueryRequirementPositions,
			len(field.Requirements),
		)
		for requirementIndex, requirement := range field.Requirements {
			cloned[fieldIndex].Requirements[requirementIndex] = requirement
			cloned[fieldIndex].Requirements[requirementIndex].Positions = slices.Clone(
				requirement.Positions,
			)
		}
	}

	return cloned
}
