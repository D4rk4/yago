package yagonode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/judgments"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/rankingprofile"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
)

const (
	pathSearchRankingTune = "/api/admin/v1/search/ranking/tune"
	// tuneNDCGCutoff scores candidate weights at rank 10, the ADR-0035 target.
	tuneNDCGCutoff = 10
)

// curatedJudgments is the read side of the judgment store the tuner trains on.
type curatedJudgments interface {
	List(ctx context.Context) ([]judgments.Judgment, error)
}

// rankingTuner fits the ranking profile to the curated judgment set by
// coordinate ascent over a local searcher, without touching the live profile —
// the operator applies the result through the ranking endpoint.
type rankingTuner struct {
	factory rankfit.SearcherFactory
	ranking *rankingprofile.Holder
	curated curatedJudgments
}

// newRankingTuner wires a tuner over the live search index. A nil index (the
// in-memory fallback deployment) yields a nil factory, so Tune reports the index
// is unavailable rather than ranking against an empty index.
func newRankingTuner(
	index searchindex.SearchIndex,
	hostRank func() hostrank.Table,
	ranking *rankingprofile.Holder,
	curated curatedJudgments,
) rankingTuner {
	var factory rankfit.SearcherFactory
	if index != nil {
		factory = func(weights searchindex.RankingWeights) searchcore.Searcher {
			return searchlocal.NewSearcherWithRanking(
				index,
				func() searchindex.RankingWeights { return weights },
				hostRank,
			)
		}
	}

	return rankingTuner{factory: factory, ranking: ranking, curated: curated}
}

// Tune fits the ranking weights to the curated judgments and returns the
// before/after report; it never persists. It needs a live index and at least one
// curated judgment.
func (t rankingTuner) Tune(ctx context.Context) (rankfit.Report, error) {
	if t.factory == nil {
		return rankfit.Report{}, fmt.Errorf("search index unavailable for tuning")
	}
	stored, err := t.curated.List(ctx)
	if err != nil {
		return rankfit.Report{}, fmt.Errorf("list judgments: %w", err)
	}
	if len(stored) == 0 {
		return rankfit.Report{}, fmt.Errorf("no curated judgments to tune against")
	}
	graded := make([]searcheval.Judgment, 0, len(stored))
	for _, judgment := range stored {
		graded = append(graded, searcheval.Judgment{
			Query:    judgment.Query,
			Relevant: judgment.Grades,
		})
	}

	options := rankfit.DefaultOptions()
	options.K = tuneNDCGCutoff
	report, err := rankfit.Fit(ctx, t.ranking.Current(), graded, t.factory, options)
	if err != nil {
		return rankfit.Report{}, fmt.Errorf("fit ranking weights: %w", err)
	}

	return report, nil
}

type searchRankingTuneEndpoint struct {
	tuner rankingTuner
}

type searchRankingTuneResponse struct {
	Before     searchindex.RankingWeights `json:"before"`
	After      searchindex.RankingWeights `json:"after"`
	BeforeNDCG float64                    `json:"beforeNdcg"`
	AfterNDCG  float64                    `json:"afterNdcg"`
	Rounds     int                        `json:"rounds"`
	Improved   bool                       `json:"improved"`
}

func newSearchRankingTuneEndpoint(tuner rankingTuner) http.Handler {
	return searchRankingTuneEndpoint{tuner: tuner}
}

// ServeHTTP runs a tune on POST and returns the before/after preview as JSON.
// The proposed weights are not applied: the operator reviews them and applies
// through the ranking endpoint.
func (e searchRankingTuneEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}
	if e.tuner.ranking == nil || e.tuner.curated == nil {
		http.Error(w, "ranking tuner unavailable", http.StatusServiceUnavailable)

		return
	}
	report, err := e.tuner.Tune(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("tune ranking: %v", err), http.StatusBadRequest)

		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(searchRankingTuneResponse{
		Before:     report.Before,
		After:      report.After,
		BeforeNDCG: report.BeforeNDCG,
		AfterNDCG:  report.AfterNDCG,
		Rounds:     report.Rounds,
		Improved:   report.Improved(),
	})
}
