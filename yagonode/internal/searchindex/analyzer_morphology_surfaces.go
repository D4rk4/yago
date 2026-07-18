package searchindex

import (
	"slices"
	"strings"

	"github.com/blevesearch/snowballstem"
)

const (
	maximumGeneratedMorphologySurfaces  = 12
	maximumMorphologyCandidateAttempts  = 2048
	maximumGeneratedMorphologyWordRunes = 32
)

type generatedMorphologySurface struct {
	value              string
	analyzerIdentities map[string]struct{}
	analyzerPriority   int
	distance           int
	lengthDifference   int
	prefixRetention    int
	ruleSupport        int
}

type morphologyAnalyzerSource struct {
	analyzer string
	stem     string
	rules    [][]*snowballstem.Among
	priority int
}

func GeneratedMorphologySurfaces(word string) []string {
	word = strings.ToLower(strings.TrimSpace(word))
	wordRunes := len([]rune(word))
	if wordRunes < 4 || wordRunes > maximumGeneratedMorphologyWordRunes {
		return normalizedGeneratedMorphologySurfaces(word, nil)
	}

	return generatedMorphologySurfaces(
		word,
		morphologyAnalyzerSources(word),
		maximumMorphologyCandidateAttempts,
	)
}

func generatedMorphologySurfaces(
	word string,
	sources []morphologyAnalyzerSource,
	maximumAttempts int,
) []string {
	candidates := make(map[string]generatedMorphologySurface)
	cycle := newMorphologyCandidateCycle(word, sources)
	for range maximumAttempts {
		proposal, available := cycle.next()
		if !available {
			break
		}
		candidate := proposal.value
		if candidate == word ||
			stemWordWithAnalyzer(candidate, proposal.analyzer) != proposal.stem {
			continue
		}
		measurement, found := candidates[candidate]
		if !found {
			measurement = generatedMorphologySurface{
				value:              candidate,
				analyzerIdentities: make(map[string]struct{}),
				analyzerPriority:   proposal.analyzerPriority,
				distance:           morphologyRuneDistance(word, candidate),
				lengthDifference:   morphologyRuneLengthDifference(word, candidate),
				prefixRetention:    morphologyRunePrefixRetention(word, candidate),
			}
		}
		measurement.analyzerIdentities[proposal.analyzer] = struct{}{}
		measurement.analyzerPriority = min(
			measurement.analyzerPriority,
			proposal.analyzerPriority,
		)
		measurement.ruleSupport = max(measurement.ruleSupport, proposal.ruleSupport)
		candidates[candidate] = measurement
	}

	return rankedGeneratedMorphologySurfaces(word, candidates)
}

func morphologyRuleSupport(rules [][]*snowballstem.Among) map[string]int {
	support := make(map[string]int)
	for _, ruleSet := range rules {
		seen := make(map[string]struct{})
		for _, rule := range ruleSet {
			suffix := strings.TrimSpace(rule.Str)
			if suffix == "" {
				continue
			}
			seen[suffix] = struct{}{}
		}
		for suffix := range seen {
			support[suffix]++
		}
	}

	return support
}

func morphologyAnalyzerSources(word string) []morphologyAnalyzerSource {
	var supported []morphologyAnalyzerSource
	for _, analyzer := range queryAnalyzers(word) {
		rules := analyzerMorphologyRules(analyzer)
		if len(rules) == 0 {
			continue
		}
		source := morphologyAnalyzerSource{
			analyzer: analyzer,
			stem:     stemWordWithAnalyzer(word, analyzer),
			rules:    rules,
		}
		supported = append(supported, source)
	}

	return prioritizedMorphologyAnalyzerSources(supported)
}

func prioritizedMorphologyAnalyzerSources(
	sources []morphologyAnalyzerSource,
) []morphologyAnalyzerSource {
	for position := range sources {
		sources[position].priority = position
	}

	return sources
}

func rankedGeneratedMorphologySurfaces(
	word string,
	candidates map[string]generatedMorphologySurface,
) []string {
	ranked := make([]generatedMorphologySurface, 0, len(candidates))
	for _, candidate := range candidates {
		ranked = append(ranked, candidate)
	}
	slices.SortFunc(ranked, compareGeneratedMorphologySurfaces)

	return normalizedGeneratedMorphologySurfaces(word, ranked)
}

func compareGeneratedMorphologySurfaces(
	left generatedMorphologySurface,
	right generatedMorphologySurface,
) int {
	if len(left.analyzerIdentities) != len(right.analyzerIdentities) {
		return len(right.analyzerIdentities) - len(left.analyzerIdentities)
	}
	if left.distance != right.distance {
		return left.distance - right.distance
	}
	if left.lengthDifference != right.lengthDifference {
		return left.lengthDifference - right.lengthDifference
	}
	if left.prefixRetention != right.prefixRetention {
		return right.prefixRetention - left.prefixRetention
	}
	if left.ruleSupport != right.ruleSupport {
		return right.ruleSupport - left.ruleSupport
	}
	if left.analyzerPriority != right.analyzerPriority {
		return left.analyzerPriority - right.analyzerPriority
	}
	return strings.Compare(left.value, right.value)
}

func morphologyRunePrefixRetention(left string, right string) int {
	retained := 0
	rightRunes := []rune(right)
	for leftPosition, leftRune := range []rune(left) {
		if leftPosition >= len(rightRunes) || leftRune != rightRunes[leftPosition] {
			break
		}
		retained++
	}

	return retained
}

func normalizedGeneratedMorphologySurfaces(
	word string,
	ranked []generatedMorphologySurface,
) []string {
	if word == "" {
		return nil
	}
	values := make([]string, 0, maximumGeneratedMorphologySurfaces)
	values = append(values, word)
	for _, candidate := range ranked {
		values = append(values, candidate.value)
		if len(values) == maximumGeneratedMorphologySurfaces {
			break
		}
	}

	return values
}

func morphologyRuneLengthDifference(left string, right string) int {
	difference := len([]rune(left)) - len([]rune(right))
	if difference < 0 {
		return -difference
	}
	return difference
}

func morphologyRuneDistance(left string, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	previous := make([]int, len(rightRunes)+1)
	current := make([]int, len(rightRunes)+1)
	for position := range previous {
		previous[position] = position
	}
	for leftPosition, leftRune := range leftRunes {
		current[0] = leftPosition + 1
		for rightPosition, rightRune := range rightRunes {
			substitution := 0
			if leftRune != rightRune {
				substitution = 1
			}
			current[rightPosition+1] = min(
				current[rightPosition]+1,
				previous[rightPosition+1]+1,
				previous[rightPosition]+substitution,
			)
		}
		previous, current = current, previous
	}

	return previous[len(rightRunes)]
}
