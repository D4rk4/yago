package documentsearch

import (
	"sort"

	"github.com/D4rk4/yago/yagoproto"
)

const maximumProtocolRequirementPositions = 64

type protocolEvidencePositions struct {
	field     string
	ordinal   int
	positions []int
}

func completeProtocolQueryFieldPositions(
	fields map[string]map[int][]int,
) []yagoproto.QueryFieldPositions {
	entries := orderedProtocolEvidencePositions(fields)
	selected := make(map[string]map[int][]int, len(fields))
	remaining := MaximumQueryMatchEvidencePositions
	covered := make(map[int]struct{})
	for _, entry := range entries {
		if remaining == 0 {
			break
		}
		if _, found := covered[entry.ordinal]; found {
			continue
		}
		selectedProtocolEvidencePosition(selected, entry, entry.positions[0])
		covered[entry.ordinal] = struct{}{}
		remaining--
	}
	for _, entry := range entries {
		if remaining == 0 {
			break
		}
		positions := selected[entry.field][entry.ordinal]
		start := 0
		if len(positions) > 0 {
			start = 1
		}
		limit := min(len(entry.positions), maximumProtocolRequirementPositions)
		for _, position := range entry.positions[start:limit] {
			if remaining == 0 {
				break
			}
			selectedProtocolEvidencePosition(selected, entry, position)
			remaining--
		}
	}

	return mappedProtocolEvidencePositions(selected)
}

func orderedProtocolEvidencePositions(
	fields map[string]map[int][]int,
) []protocolEvidencePositions {
	fieldNames := []string{"title", "headings", "anchors", "body", "url"}
	entries := make([]protocolEvidencePositions, 0)
	for _, field := range fieldNames {
		ordinals := make([]int, 0, len(fields[field]))
		for ordinal, positions := range fields[field] {
			if len(positions) > 0 {
				ordinals = append(ordinals, ordinal)
			}
		}
		sort.Ints(ordinals)
		for _, ordinal := range ordinals {
			entries = append(entries, protocolEvidencePositions{
				field:     field,
				ordinal:   ordinal,
				positions: fields[field][ordinal],
			})
		}
	}

	return entries
}

func selectedProtocolEvidencePosition(
	selected map[string]map[int][]int,
	entry protocolEvidencePositions,
	position int,
) {
	if selected[entry.field] == nil {
		selected[entry.field] = make(map[int][]int)
	}
	selected[entry.field][entry.ordinal] = append(
		selected[entry.field][entry.ordinal],
		position,
	)
}

func mappedProtocolEvidencePositions(
	selected map[string]map[int][]int,
) []yagoproto.QueryFieldPositions {
	fieldNames := []string{"title", "headings", "anchors", "body", "url"}
	mapped := make([]yagoproto.QueryFieldPositions, 0, len(selected))
	for _, field := range fieldNames {
		ordinals := make([]int, 0, len(selected[field]))
		for ordinal := range selected[field] {
			ordinals = append(ordinals, ordinal)
		}
		sort.Ints(ordinals)
		requirements := make([]yagoproto.QueryRequirementPositions, 0, len(ordinals))
		for _, ordinal := range ordinals {
			requirements = append(requirements, yagoproto.QueryRequirementPositions{
				Ordinal:   ordinal,
				Positions: selected[field][ordinal],
			})
		}
		if len(requirements) > 0 {
			mapped = append(mapped, yagoproto.QueryFieldPositions{
				Field:        field,
				Requirements: requirements,
			})
		}
	}

	return mapped
}
