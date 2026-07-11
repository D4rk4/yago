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
)

const (
	pathSearchRankingTune = "/api/admin/v1/search/ranking/tune"
	// tuneNDCGCutoff scores candidate weights at rank 10, the ADR-0035 target.
	tuneNDCGCutoff = 10
	// tuneMinClicks is the click floor a query must reach before its captured
	// clicks become an implicit judgment, keeping single-click noise out of the
	// training set (YagoRank RANK-00b).
	tuneMinClicks = 3
)

// curatedJudgments is the read side of the judgment store the tuner trains on.
type curatedJudgments interface {
	List(ctx context.Context) ([]judgments.Judgment, error)
}

// implicitJudgments derives graded judgments mined from captured result clicks;
// the clickcapture store satisfies it. It is optional — a nil source leaves the
// tuner training on the curated set alone.
type implicitJudgments interface {
	ImplicitJudgments(ctx context.Context, minClicks int) ([]searcheval.Judgment, error)
}

// rankingTuner fits the ranking profile to the curated judgment set by
// coordinate ascent over a local searcher, without touching the live profile —
// the operator applies the result through the ranking endpoint.
type rankingTuner struct {
	factory  rankfit.SearcherFactory
	ranking  *rankingprofile.Holder
	curated  curatedJudgments
	implicit implicitJudgments
}

// newRankingTuner wires a tuner over the live search index. A nil index (the
// in-memory fallback deployment) yields a nil factory, so Tune reports the index
// is unavailable rather than ranking against an empty index.
func newRankingTuner(
	index searchindex.SearchIndex,
	hostRank func() hostrank.AuthorityTable,
	ranking *rankingprofile.Holder,
	curated curatedJudgments,
	implicit implicitJudgments,
) rankingTuner {
	var factory rankfit.SearcherFactory
	if index != nil {
		factory = func(weights searchindex.RankingWeights) searchcore.Searcher {
			return searchcore.NewLexicalRerankSearcher(newLocalRankingSearcher(
				index,
				func() searchindex.RankingWeights { return weights },
				hostRank,
			))
		}
	}

	return rankingTuner{
		factory:  factory,
		ranking:  ranking,
		curated:  curated,
		implicit: implicit,
	}
}

// Tune fits the ranking weights to the curated judgments and returns the
// before/after report; it never persists. It needs a live index and at least one
// curated judgment.
func (t rankingTuner) Tune(ctx context.Context) (rankfit.Report, error) {
	if t.factory == nil {
		return rankfit.Report{}, fmt.Errorf("search index unavailable for tuning")
	}
	graded, err := t.trainingJudgments(ctx)
	if err != nil {
		return rankfit.Report{}, err
	}
	if len(graded) == 0 {
		return rankfit.Report{}, fmt.Errorf("no judgments to tune against")
	}

	options := rankfit.DefaultOptions()
	options.K = tuneNDCGCutoff
	report, err := rankfit.Fit(ctx, t.ranking.Current(), graded, t.factory, options)
	if err != nil {
		return rankfit.Report{}, fmt.Errorf("fit ranking weights: %w", err)
	}

	return report, nil
}

// trainingJudgments assembles the tuner's training set: curated judgments are
// authoritative, and implicit judgments mined from result clicks fill in queries
// the operator has not curated. Curated wins wholesale on a query, so a human
// label is never diluted by click noise.
func (t rankingTuner) trainingJudgments(
	ctx context.Context,
) ([]searcheval.Judgment, error) {
	stored, err := t.curated.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list judgments: %w", err)
	}
	graded := make([]searcheval.Judgment, 0, len(stored))
	curatedQueries := make(map[string]struct{}, len(stored))
	for _, judgment := range stored {
		curatedQueries[judgment.Query] = struct{}{}
		graded = append(graded, searcheval.Judgment{
			Query:          judgment.Query,
			QueryCluster:   judgment.QueryCluster,
			ObservedAt:     judgment.ObservedAt,
			Relevant:       judgment.Grades,
			ClusterIntents: judgment.ClusterIntents,
			Navigational:   judgment.Navigational,
			SliceNames:     judgment.SliceNames,
		})
	}
	if t.implicit == nil {
		return graded, nil
	}
	mined, err := t.implicit.ImplicitJudgments(ctx, tuneMinClicks)
	if err != nil {
		return nil, fmt.Errorf("mine implicit judgments: %w", err)
	}
	for _, judgment := range mined {
		if _, curated := curatedQueries[judgment.Query]; !curated {
			graded = append(graded, judgment)
		}
	}

	return graded, nil
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
