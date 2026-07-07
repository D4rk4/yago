package searchindex

import (
	"strings"

	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

// Sequential Dependence Model bigram boosts (Metzler & Croft, SIGIR 2005):
// beside the independent-term score, documents where adjacent query words
// appear as an ordered window rank higher. The canonical SDM mixes unigram,
// ordered-window, and unordered-window features at 0.85/0.10/0.05; on top of
// BM25 unigrams the ordered feature is approximated with phrase clauses over
// adjacent query-word pairs, weighted at sdmBigramWeight of the field weight.
// The unordered-window feature is skipped: bleve has no cheap unordered
// operator, and SDM ablations attribute most of the gain to the ordered one.
const sdmBigramWeight = 0.12

// sdmBigramBoosts returns SHOULD-clauses boosting documents that contain
// adjacent query-word pairs as phrases in the title or body. Queries with
// fewer than two terms carry no dependency signal.
func sdmBigramBoosts(
	terms []string,
	weights RankingWeights,
	analyzer string,
) []blevequery.Query {
	pairs := adjacentPairs(terms)
	boosts := make([]blevequery.Query, 0, len(pairs))
	for _, pair := range pairs {
		boosts = append(boosts, bleve.NewDisjunctionQuery(
			fieldPhrase("title", pair, weights.Title*sdmBigramWeight, analyzer),
			fieldPhrase("body", pair, weights.Body*sdmBigramWeight, analyzer),
		))
	}

	return boosts
}

// adjacentPairs joins each pair of neighboring non-empty query words.
func adjacentPairs(terms []string) []string {
	cleaned := make([]string, 0, len(terms))
	for _, term := range terms {
		if term = strings.TrimSpace(term); term != "" {
			cleaned = append(cleaned, term)
		}
	}
	if len(cleaned) < 2 {
		return nil
	}
	pairs := make([]string, 0, len(cleaned)-1)
	for i := 0; i+1 < len(cleaned); i++ {
		pairs = append(pairs, cleaned[i]+" "+cleaned[i+1])
	}

	return pairs
}
