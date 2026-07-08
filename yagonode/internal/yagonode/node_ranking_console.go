package yagonode

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/rankingprofile"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

// ranker fits the ranking weights to the judgment set; the concrete tuner
// satisfies it, and a fake stands in for it under test.
type ranker interface {
	Tune(ctx context.Context) (rankfit.Report, error)
}

// rankingWeightMeta is the console's display order, label, and group for each
// ranking weight. The values themselves come from the live profile keyed by the
// weight's JSON field name, so the weight set stays defined once (on
// RankingWeights) and this list only carries presentation.
var rankingWeightMeta = []struct{ key, label, group string }{
	{"title", "Title", "Field boosts"},
	{"anchors", "Anchor text", "Field boosts"},
	{"headings", "Headings", "Field boosts"},
	{"url", "URL", "Field boosts"},
	{"body", "Body", "Field boosts"},
	{"hostRank", "Host authority", "Priors"},
	{"freshness", "Freshness", "Priors"},
	{"quality", "Content quality", "Priors"},
	{"proximity", "Proximity (SDM)", "Priors"},
}

// rankingConsole adapts the ranking holder, the coordinate-ascent tuner, and the
// judgment store to the admin console's RankingSource.
type rankingConsole struct {
	holder  *rankingprofile.Holder
	tuner   ranker
	curated curatedJudgments
}

// newRankingConsole wires the console section; a nil holder (the in-memory
// fallback deployment carries no persisted profile) yields a nil source so the
// section renders its unavailable state.
func newRankingConsole(
	holder *rankingprofile.Holder,
	tuner ranker,
	curated curatedJudgments,
) adminui.RankingSource {
	if holder == nil {
		return nil
	}

	return &rankingConsole{holder: holder, tuner: tuner, curated: curated}
}

func (rc *rankingConsole) Profile(ctx context.Context) adminui.RankingProfile {
	return adminui.RankingProfile{
		Weights:       weightsView(rc.holder.Current()),
		JudgmentCount: rc.judgmentCount(ctx),
	}
}

// judgmentCount reports how many curated judgments the tuner would train on; a
// missing store or a read error degrades to zero rather than failing the page.
func (rc *rankingConsole) judgmentCount(ctx context.Context) int {
	if rc.curated == nil {
		return 0
	}
	stored, err := rc.curated.List(ctx)
	if err != nil {
		return 0
	}

	return len(stored)
}

func (rc *rankingConsole) Tune(ctx context.Context) (adminui.RankingTuneResult, error) {
	report, err := rc.tuner.Tune(ctx)
	if err != nil {
		return adminui.RankingTuneResult{}, fmt.Errorf("tune ranking: %w", err)
	}

	return adminui.RankingTuneResult{
		BeforeNDCG: report.BeforeNDCG,
		AfterNDCG:  report.AfterNDCG,
		Rounds:     report.Rounds,
		Improved:   report.Improved(),
		Proposed:   weightsView(report.After),
	}, nil
}

func (rc *rankingConsole) Apply(ctx context.Context, values map[string]float64) error {
	weights := weightsFromMap(rc.holder.Current(), values)
	if err := weights.Validate(); err != nil {
		return fmt.Errorf("validate weights: %w", err)
	}
	if err := rc.holder.Set(ctx, weights); err != nil {
		return fmt.Errorf("save ranking profile: %w", err)
	}

	return nil
}

// weightsView renders the live weights in the console's display order, reading
// each value from the profile by its JSON key.
func weightsView(weights searchindex.RankingWeights) []adminui.RankingWeight {
	values := weightsToMap(weights)
	view := make([]adminui.RankingWeight, 0, len(rankingWeightMeta))
	for _, meta := range rankingWeightMeta {
		view = append(view, adminui.RankingWeight{
			Key:   meta.key,
			Label: meta.label,
			Group: meta.group,
			Value: values[meta.key],
		})
	}

	return view
}

// weightsToMap projects the ranking weights onto their JSON field names so the
// console reads and writes them by key without re-listing every field.
func weightsToMap(weights searchindex.RankingWeights) map[string]float64 {
	encoded, _ := json.Marshal(weights)
	values := map[string]float64{}
	_ = json.Unmarshal(encoded, &values)

	return values
}

// weightsFromMap overlays the supplied values onto the base weights by JSON key,
// so keys the form omits keep their current value.
func weightsFromMap(
	base searchindex.RankingWeights,
	overlay map[string]float64,
) searchindex.RankingWeights {
	values := weightsToMap(base)
	for key, value := range overlay {
		values[key] = value
	}
	encoded, _ := json.Marshal(values)
	var weights searchindex.RankingWeights
	_ = json.Unmarshal(encoded, &weights)

	return weights
}
