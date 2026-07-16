package searchremote

import (
	"cmp"
	"slices"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func rankRemoteResults(results []searchcore.Result) []searchcore.Result {
	slices.SortStableFunc(results, func(a, b searchcore.Result) int {
		return cmp.Or(
			cmp.Compare(b.Score, a.Score),
			strings.Compare(remoteResultIdentity(a), remoteResultIdentity(b)),
		)
	})

	return results
}

// RankingWeights is the slice of the local ranking profile that remote
// results can honor: peers return only URL metadata, so the title and the URL
// text are the only rankable fields. The defaults mirror the local
// searchindex defaults for the same fields.
type RankingWeights struct {
	Title float64
	URL   float64
}

func DefaultRankingWeights() RankingWeights {
	return RankingWeights{Title: 6, URL: 2}
}

const (
	// remoteProfileShare is the score share earned by matching query terms
	// against the fields the local ranking profile weighs, while
	// remotePeerOrderShare keeps the sending peer's own result order as a
	// tiebreak: the peer ranked against the full document text, which URL
	// metadata no longer carries.
	remoteProfileShare   = 0.95
	remotePeerOrderShare = 0.05
)

// remoteScorer ranks remote results with the local ranking profile, like
// YaCy's SearchEvent scores remote entries through the local ReferenceOrder
// instead of trusting the sending peer's profile. Scores stay in [0, 1] so
// the federated merge can calibrate them against the local score scale.
type remoteScorer struct {
	terms   []string
	weights RankingWeights
}

func newRemoteScorer(terms []string, weights RankingWeights) remoteScorer {
	lowered := make([]string, 0, len(terms))
	for _, term := range terms {
		lowered = append(lowered, strings.ToLower(term))
	}

	return remoteScorer{terms: lowered, weights: weights}
}

func (s remoteScorer) score(result searchcore.Result, rank int, total int) float64 {
	return remoteProfileShare*s.profileMatchScore(result) +
		remotePeerOrderShare*peerOrderScore(rank, total)
}

func (s remoteScorer) profileMatchScore(result searchcore.Result) float64 {
	denominator := (s.weights.Title + s.weights.URL) * float64(len(s.terms))
	if denominator <= 0 {
		return 0
	}
	title := searchcore.NewVisibleTextTerms(result.Title)
	location := searchcore.NewVisibleURLTerms(result.URL)
	matched := 0.0
	for _, term := range s.terms {
		if title.Mentions(term) {
			matched += s.weights.Title
		}
		if location.Mentions(term) {
			matched += s.weights.URL
		}
	}

	return matched / denominator
}

func peerOrderScore(rank int, total int) float64 {
	if total <= 0 || rank < 0 || rank >= total {
		return 0
	}

	return float64(total-rank) / float64(total)
}

// weightsOrDefault substitutes the default weights only for a missing
// provider. A provider returning zero title and URL weights is a deliberate
// profile choice (the admin ranks by other fields), which leaves remote
// results ordered by the peer-order tiebreak alone.
func weightsOrDefault(weights func() RankingWeights) func() RankingWeights {
	if weights == nil {
		return DefaultRankingWeights
	}

	return weights
}
