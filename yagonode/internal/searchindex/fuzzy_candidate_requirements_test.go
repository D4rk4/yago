package searchindex

import (
	"fmt"
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/mapping"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type observedFuzzyCandidateReader struct {
	*scorch.IndexSnapshot
	terms []string
}

func (reader *observedFuzzyCandidateReader) FieldDictFuzzyAutomaton(
	field, term string,
	distance int,
	prefix string,
) (bleveindex.FieldDict, bleveindex.FuzzyAutomaton, error) {
	reader.terms = append(reader.terms, term)
	dictionary, automaton, err := reader.IndexSnapshot.FieldDictFuzzyAutomaton(
		field,
		term,
		distance,
		prefix,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("open observed fuzzy dictionary: %w", err)
	}

	return dictionary, automaton, nil
}

func TestFuzzyCandidatesStopAtMissingRequirement(t *testing.T) {
	snapshot := newBleveTermPostingsFixture(t)
	for _, tc := range []struct {
		terms               []string
		lastOpened, matches bool
	}{
		{[]string{"quartzite", "alpha", "beta"}, false, false},
		{[]string{"alpha", "quartzite", "beta"}, false, false},
		{[]string{"alpha", "beta"}, true, true},
	} {
		reader := &observedFuzzyCandidateReader{IndexSnapshot: snapshot}
		query := bleveLexicalRequirementCandidateBranch(
			SearchRequest{Fuzzy: true, Terms: tc.terms},
			"standard",
			DefaultRankingWeights(),
		)
		ids, complete, err := collectBleveLexicalCandidateSnapshot(
			t.Context(),
			reader,
			mapping.NewIndexMapping(),
			query,
			100,
		)
		if err != nil || !complete || (len(ids) > 0) != tc.matches ||
			slices.Contains(reader.terms, "beta") != tc.lastOpened {
			t.Errorf(
				"requirements %v: identities=%v complete=%t error=%v scanned=%v",
				tc.terms,
				ids,
				complete,
				err,
				reader.terms,
			)
		}
	}
}
