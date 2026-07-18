package searchindex

import (
	"strings"

	"github.com/blevesearch/snowballstem"
)

type generatedMorphologyProposal struct {
	value            string
	analyzer         string
	stem             string
	analyzerPriority int
	ruleSupport      int
}

type morphologyCandidateIterator struct {
	source          morphologyAnalyzerSource
	roots           []string
	ruleSupport     map[string]int
	rootPosition    int
	ruleSetPosition int
	rulePosition    int
}

func newMorphologyCandidateIterator(
	word string,
	source morphologyAnalyzerSource,
) morphologyCandidateIterator {
	return morphologyCandidateIterator{
		source:      source,
		roots:       morphologySurfaceRoots(word, source.stem, source.rules),
		ruleSupport: morphologyRuleSupport(source.rules),
	}
}

func (iterator *morphologyCandidateIterator) next() (
	generatedMorphologyProposal,
	bool,
) {
	for iterator.rootPosition < len(iterator.roots) {
		for iterator.ruleSetPosition < len(iterator.source.rules) {
			rules := iterator.source.rules[iterator.ruleSetPosition]
			for iterator.rulePosition < len(rules) {
				rule := rules[iterator.rulePosition]
				iterator.rulePosition++
				suffix := strings.TrimSpace(rule.Str)
				if suffix == "" {
					continue
				}

				return generatedMorphologyProposal{
					value:            iterator.roots[iterator.rootPosition] + suffix,
					analyzer:         iterator.source.analyzer,
					stem:             iterator.source.stem,
					analyzerPriority: iterator.source.priority,
					ruleSupport:      iterator.ruleSupport[suffix],
				}, true
			}
			iterator.ruleSetPosition++
			iterator.rulePosition = 0
		}
		iterator.rootPosition++
		iterator.ruleSetPosition = 0
	}

	return generatedMorphologyProposal{}, false
}

type morphologyCandidateCycle struct {
	iterators    []morphologyCandidateIterator
	nextPosition int
}

func newMorphologyCandidateCycle(
	word string,
	sources []morphologyAnalyzerSource,
) *morphologyCandidateCycle {
	cycle := &morphologyCandidateCycle{
		iterators: make([]morphologyCandidateIterator, len(sources)),
	}
	for position, source := range sources {
		cycle.iterators[position] = newMorphologyCandidateIterator(word, source)
	}

	return cycle
}

func (cycle *morphologyCandidateCycle) next() (generatedMorphologyProposal, bool) {
	for range len(cycle.iterators) {
		position := cycle.nextPosition
		cycle.nextPosition = (cycle.nextPosition + 1) % len(cycle.iterators)
		proposal, available := cycle.iterators[position].next()
		if available {
			return proposal, true
		}
	}

	return generatedMorphologyProposal{}, false
}

func morphologySurfaceRoots(
	word string,
	stem string,
	rules [][]*snowballstem.Among,
) []string {
	roots := []string{stem}
	seen := map[string]struct{}{stem: {}}
	for _, ruleSet := range rules {
		for _, rule := range ruleSet {
			suffix := strings.TrimSpace(rule.Str)
			root, found := strings.CutSuffix(word, suffix)
			if !found || len([]rune(root)) < 2 {
				continue
			}
			if _, known := seen[root]; known {
				continue
			}
			seen[root] = struct{}{}
			roots = append(roots, root)
		}
	}

	return roots
}
