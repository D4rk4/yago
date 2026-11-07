package searchindex

import (
	"sort"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2/search"
)

type snippetWitness struct {
	text       string
	arrayIndex uint64
	evidence   TextQueryEvidence
	coverage   int
	spread     int
}

type snippetOccurrence struct {
	arrayIndex uint64
	location   textQueryLocation
	runeStart  int
	runeEnd    int
}

func (w snippetWitness) strongerThan(other snippetWitness) bool {
	if w.coverage != other.coverage {
		return w.coverage > other.coverage
	}

	return w.spread < other.spread
}

func fieldSnippetEvidence(
	terms search.TermLocationMap,
	values []string,
	allowedTerms map[string]struct{},
	acceptAnyTerm bool,
) (snippetWitness, bool) {
	occurrences := []snippetOccurrence{}
	requirements := 0
	for term, locations := range terms {
		if _, allowed := allowedTerms[term]; !acceptAnyTerm && !allowed {
			continue
		}
		for _, location := range locations {
			if occurrence, valid := locationSnippetOccurrence(
				values,
				location,
				requirements,
			); valid {
				occurrences = append(occurrences, occurrence)
			}
		}
		requirements++
	}

	return strongestSnippetWitness(values, occurrences, requirements)
}

func locationSnippetOccurrence(
	values []string,
	location *search.Location,
	requirement int,
) (snippetOccurrence, bool) {
	if location == nil || len(values) == 0 || location.End <= location.Start {
		return snippetOccurrence{}, false
	}
	arrayIndex := uint64(0)
	if len(location.ArrayPositions) > 0 {
		arrayIndex = location.ArrayPositions[0]
	}
	if arrayIndex >= uint64(len(values)) {
		return snippetOccurrence{}, false
	}
	value := values[arrayIndex]
	if location.End > uint64(len(value)) {
		return snippetOccurrence{}, false
	}
	start := len(value[:location.Start])
	end := len(value[:location.End])
	if !utf8.ValidString(value[start:end]) {
		return snippetOccurrence{}, false
	}

	return snippetOccurrence{
		arrayIndex: arrayIndex,
		location:   textQueryLocation{requirement: requirement, start: start, end: end},
	}, true
}

func strongestSnippetWitness(
	values []string,
	occurrences []snippetOccurrence,
	requirements int,
) (snippetWitness, bool) {
	sort.Slice(occurrences, func(left int, right int) bool {
		if occurrences[left].arrayIndex != occurrences[right].arrayIndex {
			return occurrences[left].arrayIndex < occurrences[right].arrayIndex
		}
		if occurrences[left].location.start != occurrences[right].location.start {
			return occurrences[left].location.start < occurrences[right].location.start
		}

		return occurrences[left].location.end > occurrences[right].location.end
	})
	counts := make([]int, requirements)
	var strongest snippetWitness
	for first := 0; first < len(occurrences); {
		last := first + 1
		for last < len(occurrences) &&
			occurrences[last].arrayIndex == occurrences[first].arrayIndex {
			last++
		}
		candidate := strongestValueWitness(
			values[occurrences[first].arrayIndex],
			occurrences[first:last],
			counts,
		)
		if candidate.strongerThan(strongest) {
			strongest = candidate
		}
		first = last
	}

	return strongest, len(occurrences) > 0
}

func strongestValueWitness(
	value string,
	occurrences []snippetOccurrence,
	counts []int,
) snippetWitness {
	measureOccurrenceRunes(value, occurrences)
	covered := 0
	left := 0
	reach := newSnippetWindowReach(occurrences)
	var strongest snippetWitness
	for right, occurrence := range occurrences {
		if counts[occurrence.location.requirement] == 0 {
			covered++
		}
		counts[occurrence.location.requirement]++
		reach.include(right)
		for left < right &&
			(reach.furthest().runeEnd-occurrences[left].runeStart > snippetRuneCap ||
				counts[occurrences[left].location.requirement] > 1) {
			counts[occurrences[left].location.requirement]--
			if counts[occurrences[left].location.requirement] == 0 {
				covered--
			}
			left++
			reach.startAt(left)
		}
		end := reach.furthest().location.end
		if right+1 < len(occurrences) && occurrences[right+1].location.start < end {
			continue
		}
		candidate := snippetWitness{
			text:       value,
			arrayIndex: occurrence.arrayIndex,
			evidence:   TextQueryEvidence{Start: occurrences[left].location.start, End: end},
			coverage:   covered,
			spread:     occurrence.runeStart - occurrences[left].runeStart,
		}
		if candidate.strongerThan(strongest) {
			strongest = candidate
		}
	}
	for _, occurrence := range occurrences {
		counts[occurrence.location.requirement] = 0
	}

	return strongest
}

func measureOccurrenceRunes(value string, occurrences []snippetOccurrence) {
	previous := occurrences[0].location.start
	runeStart := 0
	for index := range occurrences {
		location := occurrences[index].location
		runeStart += utf8.RuneCountInString(value[previous:location.start])
		occurrences[index].runeStart = runeStart
		occurrences[index].runeEnd = runeStart +
			utf8.RuneCountInString(value[location.start:location.end])
		previous = location.start
	}
}
