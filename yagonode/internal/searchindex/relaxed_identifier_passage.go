package searchindex

import (
	"cmp"
	"slices"

	"github.com/blevesearch/bleve/v2/search"
)

type relaxedPassagePosition struct {
	requirement int
	position    uint64
}

func relaxedPassageSupportsExactIdentifiers(
	latest map[int]*search.Location,
	minimum int,
	identifiers []int,
) bool {
	positions := make([]relaxedPassagePosition, 0, len(latest))
	for requirement, location := range latest {
		positions = append(positions, relaxedPassagePosition{
			requirement: requirement,
			position:    location.Pos,
		})
	}
	slices.SortFunc(positions, func(left, right relaxedPassagePosition) int {
		return cmp.Or(
			cmp.Compare(left.position, right.position),
			cmp.Compare(left.requirement, right.requirement),
		)
	})
	maximumSpan := uint64(max(sdmUnorderedWindow, minimum*sdmUnorderedWindow))
	for start := 0; start+minimum <= len(positions); start++ {
		end := start + minimum - 1
		if positions[end].position-positions[start].position <= maximumSpan &&
			relaxedPassageContainsIdentifiers(positions[start:end+1], identifiers) {
			return true
		}
	}

	return false
}

func relaxedPassageContainsIdentifiers(
	positions []relaxedPassagePosition,
	identifiers []int,
) bool {
	for _, identifier := range identifiers {
		found := false
		for _, position := range positions {
			if position.requirement == identifier {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
