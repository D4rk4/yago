package adminui

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// RankingSource backs the YagoRank console section: it reads the live ranking
// profile, fits it to the judgment set by coordinate ascent, and applies the
// weights an operator reviews.
type RankingSource interface {
	// Profile returns the live ranking weights and the judgment-set size.
	Profile(ctx context.Context) RankingProfile
	// Tune fits the weights to the judgment set and returns the before/after
	// preview without persisting anything; the operator applies it via Apply.
	Tune(ctx context.Context) (RankingTuneResult, error)
	// Apply persists the given weight values keyed by RankingWeight.Key; it
	// returns an error when the resulting profile fails validation.
	Apply(ctx context.Context, weights map[string]float64) error
}

// RankingWeight is one tunable ranking weight for display and editing.
type RankingWeight struct {
	Key   string
	Label string
	Group string
	Value float64
}

// RankingProfile is the live ranking profile plus the judgment-set size.
type RankingProfile struct {
	Weights       []RankingWeight
	JudgmentCount int
}

// RankingTuneResult is a coordinate-ascent preview: the mean NDCG@10 before and
// after tuning, how many rounds ran, whether it improved, and the proposed
// weights the operator can review and save.
type RankingTuneResult struct {
	BeforeNDCG float64
	AfterNDCG  float64
	Rounds     int
	Improved   bool
	Proposed   []RankingWeight
}

const yagorankUnavailable = "YagoRank tuning is not available on this node."

type rankingWeightGroup struct {
	Title   string
	Weights []RankingWeight
}

type yagorankPageData struct {
	AppName       string
	ActivePath    string
	Nav           []NavItem
	CSRF          string
	Section       sectionView
	Groups        []rankingWeightGroup
	JudgmentCount int
	Tune          *RankingTuneResult
	LearnedModel  *learnedModelStatusView
	TrainOutcome  *LearnedModelTrainOutcome
	HostTrust     *hostTrustStatusView
	Notice        string
	Error         string
}

// yagorankView carries the variable parts of a YagoRank render: the weights to
// show in the inputs, an optional tune preview, and the toast messages.
type yagorankView struct {
	weights      []RankingWeight
	tune         *RankingTuneResult
	trainOutcome *LearnedModelTrainOutcome
	trust        *hostTrustStatusView
	notice       string
	errMsg       string
}

func (c *Console) handleYagoRank(w http.ResponseWriter, r *http.Request) {
	if c.ranking == nil {
		c.renderUnavailable(w, r, yagorankPath, "YagoRank", yagorankUnavailable)

		return
	}

	c.renderYagoRank(w, r, yagorankView{weights: c.ranking.Profile(r.Context()).Weights})
}

func (c *Console) handleYagoRankAction(w http.ResponseWriter, r *http.Request) {
	if c.ranking == nil {
		c.renderUnavailable(w, r, yagorankPath, "YagoRank", yagorankUnavailable)

		return
	}

	switch r.PostFormValue("action") {
	case "tune":
		c.applyYagoRankTune(w, r)
	case "save":
		c.applyYagoRankSave(w, r)
	case "train-linear":
		c.applyYagoRankModelTraining(w, r, LearnedModelLinearLambdaRank)
	case "train-tree":
		c.applyYagoRankModelTraining(w, r, LearnedModelHistogramLambdaMART)
	case "rollback-model":
		c.applyYagoRankModelRollback(w, r)
	case "save-trust":
		c.applyYagoRankHostTrust(w, r)
	default:
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			errMsg:  "Unknown action.",
		})
	}
}

func (c *Console) applyYagoRankTune(w http.ResponseWriter, r *http.Request) {
	result, err := c.ranking.Tune(r.Context())
	if err != nil {
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			errMsg:  "Tuning failed: " + err.Error(),
		})

		return
	}
	// Pre-fill the inputs with the proposed weights so a following Save applies
	// them; the preview panel reports the NDCG gain.
	c.renderYagoRank(w, r, yagorankView{
		weights: result.Proposed,
		tune:    &result,
		notice:  tuneNotice(result),
	})
}

func (c *Console) applyYagoRankSave(w http.ResponseWriter, r *http.Request) {
	profile := c.ranking.Profile(r.Context())
	weights := make(map[string]float64, len(profile.Weights))
	for _, weight := range profile.Weights {
		raw := strings.TrimSpace(r.PostFormValue(weight.Key))
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			c.renderYagoRank(w, r, yagorankView{
				weights: profile.Weights,
				errMsg:  "Enter a number for " + weight.Label + ".",
			})

			return
		}
		weights[weight.Key] = value
	}
	if err := c.ranking.Apply(r.Context(), weights); err != nil {
		c.renderYagoRank(w, r, yagorankView{
			weights: profile.Weights,
			errMsg:  "Save failed: " + err.Error(),
		})

		return
	}
	c.renderYagoRank(w, r, yagorankView{
		weights: c.ranking.Profile(r.Context()).Weights,
		notice:  "Ranking profile saved.",
	})
}

func (c *Console) renderYagoRank(w http.ResponseWriter, r *http.Request, view yagorankView) {
	c.render(r.Context(), w, c.tpl.yagorank, "layout", yagorankPageData{
		AppName: appName, ActivePath: yagorankPath, Nav: navItems,
		CSRF:          csrfToken(r),
		Section:       sectionView{Heading: "YagoRank", Available: true},
		Groups:        groupRankingWeights(view.weights),
		JudgmentCount: c.ranking.Profile(r.Context()).JudgmentCount,
		Tune:          view.tune,
		LearnedModel:  learnedModelStatus(r.Context(), c.ranking),
		TrainOutcome:  view.trainOutcome,
		HostTrust:     hostTrustStatus(r.Context(), c.ranking, view.trust),
		Notice:        view.notice,
		Error:         view.errMsg,
	})
}

// groupRankingWeights buckets the weights into their display groups, preserving
// first-seen order so field boosts and priors render as separate fieldsets.
func groupRankingWeights(weights []RankingWeight) []rankingWeightGroup {
	groups := make([]rankingWeightGroup, 0, 2)
	index := map[string]int{}
	for _, weight := range weights {
		position, ok := index[weight.Group]
		if !ok {
			position = len(groups)
			index[weight.Group] = position
			groups = append(groups, rankingWeightGroup{Title: weight.Group})
		}
		groups[position].Weights = append(groups[position].Weights, weight)
	}

	return groups
}

func tuneNotice(result RankingTuneResult) string {
	if result.Improved {
		return fmt.Sprintf(
			"Tuning lifted mean NDCG@10 from %.4f to %.4f over %d rounds.",
			result.BeforeNDCG, result.AfterNDCG, result.Rounds,
		)
	}

	return fmt.Sprintf(
		"Tuning found no improvement over the current weights (NDCG@10 %.4f).",
		result.BeforeNDCG,
	)
}
