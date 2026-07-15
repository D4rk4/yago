package searchremote

import (
	"math"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func almostEqual(a float64, b float64) bool {
	return math.Abs(a-b) < 1e-12
}

func TestDefaultRankingWeightsMatchLocalProfileFields(t *testing.T) {
	if got := DefaultRankingWeights(); got != (RankingWeights{Title: 6, URL: 2}) {
		t.Fatalf("default remote weights = %#v", got)
	}
}

func TestRemoteScorerAppliesLocalProfileToTitleAndURL(t *testing.T) {
	scorer := newRemoteScorer([]string{"Go", "tutorial"}, RankingWeights{Title: 4, URL: 1})
	for _, item := range []struct {
		name   string
		result searchcore.Result
		want   float64
	}{
		{
			name: "both terms in title and url",
			result: searchcore.Result{
				Title: "Go Tutorial",
				URL:   "https://example.org/go/tutorial",
			},
			want: remoteProfileShare + remotePeerOrderShare,
		},
		{
			name:   "one term in title only",
			result: searchcore.Result{Title: "The Go Book", URL: "https://example.org/book"},
			want:   remoteProfileShare*(4.0/10.0) + remotePeerOrderShare,
		},
		{
			name:   "one term in url only",
			result: searchcore.Result{Title: "Untitled", URL: "https://example.org/tutorial"},
			want:   remoteProfileShare*(1.0/10.0) + remotePeerOrderShare,
		},
		{
			name:   "no match keeps only the peer-order share",
			result: searchcore.Result{Title: "Untitled", URL: "https://example.org/other"},
			want:   remotePeerOrderShare,
		},
	} {
		if got := scorer.score(item.result, 0, 1); !almostEqual(got, item.want) {
			t.Fatalf("%s: score = %v, want %v", item.name, got, item.want)
		}
	}
}

func TestRemoteScorerTitleWeightOutranksPeerOrder(t *testing.T) {
	scorer := newRemoteScorer([]string{"golang"}, DefaultRankingWeights())
	matching := scorer.score(
		searchcore.Result{Title: "Golang guide", URL: "https://example.org/guide"},
		9,
		10,
	)
	first := scorer.score(
		searchcore.Result{Title: "Unrelated", URL: "https://example.org/other"},
		0,
		10,
	)
	if matching <= first {
		t.Fatalf(
			"profile match at the peer's last position (%v) should outrank a"+
				" non-match at its first (%v)",
			matching,
			first,
		)
	}
}

func TestRemoteScorerZeroWeightsRankByPeerOrderAlone(t *testing.T) {
	scorer := newRemoteScorer([]string{"golang"}, RankingWeights{})
	got := scorer.score(
		searchcore.Result{Title: "Golang", URL: "https://example.org/golang"},
		1,
		4,
	)
	if want := remotePeerOrderShare * (3.0 / 4.0); !almostEqual(got, want) {
		t.Fatalf("score = %v, want %v", got, want)
	}
}

func TestRemoteScorerNoTermsRankByPeerOrderAlone(t *testing.T) {
	scorer := newRemoteScorer(nil, DefaultRankingWeights())
	if got := scorer.score(
		searchcore.Result{Title: "Anything"},
		0,
		2,
	); got != remotePeerOrderShare {
		t.Fatalf("score = %v, want %v", got, remotePeerOrderShare)
	}
}

func TestPeerOrderScoreBounds(t *testing.T) {
	for _, item := range []struct {
		name  string
		rank  int
		total int
		want  float64
	}{
		{name: "first of ten", rank: 0, total: 10, want: 1},
		{name: "last of ten", rank: 9, total: 10, want: 0.1},
		{name: "zero total", rank: 0, total: 0, want: 0},
		{name: "negative rank", rank: -1, total: 10, want: 0},
		{name: "rank beyond total", rank: 10, total: 10, want: 0},
	} {
		if got := peerOrderScore(item.rank, item.total); got != item.want {
			t.Fatalf("%s: peerOrderScore = %v, want %v", item.name, got, item.want)
		}
	}
}

func TestWeightsOrDefaultKeepsDeliberateZeroProfile(t *testing.T) {
	if got := weightsOrDefault(nil)(); got != DefaultRankingWeights() {
		t.Fatalf("nil provider weights = %#v", got)
	}
	zero := func() RankingWeights { return RankingWeights{} }
	if got := weightsOrDefault(zero)(); got != (RankingWeights{}) {
		t.Fatalf("deliberate zero profile replaced with %#v", got)
	}
}
