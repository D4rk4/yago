package rankingtrain

import (
	"context"
	"errors"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

func TestBuildProposalRejectsInvalidInputsAndRetrievalFailures(t *testing.T) {
	valid := Config{Revision: "revision"}
	invalidFamily := valid
	invalidFamily.Family = ModelFamily("future")
	if _, err := BuildProposal(t.Context(), nil, nil, invalidFamily); err == nil ||
		!strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("family error = %v", err)
	}
	var missingContext context.Context
	if _, err := BuildProposal(missingContext, &scriptedSearcher{}, nil, valid); err == nil ||
		!strings.Contains(err.Error(), "context") {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := BuildProposal(t.Context(), nil, nil, valid); err == nil ||
		!strings.Contains(err.Error(), "searcher") {
		t.Fatalf("nil searcher error = %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := BuildProposal(cancelled, &scriptedSearcher{}, nil, valid); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("initial cancellation error = %v", err)
	}
	if _, err := BuildProposal(t.Context(), &scriptedSearcher{}, nil, valid); err == nil ||
		!strings.Contains(err.Error(), "judgments") {
		t.Fatalf("retrieval error = %v", err)
	}
	if _, err := BuildProposal(
		t.Context(),
		&scriptedSearcher{results: map[string][]searchcore.Result{"query": {
			rankingFixtureResult("https://only.example", 1, 1),
		}}},
		[]searcheval.Judgment{{Query: "query"}},
		valid,
	); err == nil || !strings.Contains(err.Error(), "splits must not be empty") {
		t.Fatalf("split error = %v", err)
	}
	invalidRevision := valid
	invalidRevision.Revision = "!invalid"
	if _, err := BuildProposal(
		t.Context(),
		&scriptedSearcher{},
		nil,
		invalidRevision,
	); err == nil ||
		!strings.Contains(err.Error(), "revision") {
		t.Fatalf("revision error = %v", err)
	}
	invalidIncumbent := valid
	emptySnapshot := learnedrank.Snapshot{}
	invalidIncumbent.Incumbent = &emptySnapshot
	if _, err := BuildProposal(
		t.Context(),
		&scriptedSearcher{},
		nil,
		invalidIncumbent,
	); err == nil || !strings.Contains(err.Error(), "active incumbent") {
		t.Fatalf("incumbent error = %v", err)
	}
	sentinel := errors.New("model factory failed")
	if err := validateModelRevision(
		"revision",
		func(
			[]rankfit.FeatureDefinition,
			[]float64,
		) (rankfit.LinearLambdaRankModel, error) {
			return rankfit.LinearLambdaRankModel{}, sentinel
		},
	); !errors.Is(err, sentinel) {
		t.Fatalf("model factory error = %v", err)
	}
}

func TestBuildProposalPropagatesTrainingAndPromotionFailures(t *testing.T) {
	judgments, searcher := rankingFixture()
	for index := range judgments {
		judgments[index].Relevant = map[string]int{}
	}
	if _, err := BuildProposal(
		t.Context(),
		searcher,
		judgments,
		Config{Revision: "no-preferences"},
	); err == nil || !strings.Contains(err.Error(), "no preference evidence") {
		t.Fatalf("training error = %v", err)
	}

	judgments, searcher = rankingFixture()
	config := DefaultConfig("invalid-policy", FamilyLinearLambdaRank)
	config.PromotionPolicy.MinimumRelativeNDCGGain = math.NaN()
	if _, err := BuildProposal(t.Context(), searcher, judgments, config); err == nil ||
		!strings.Contains(err.Error(), "decide held-out promotion") {
		t.Fatalf("promotion error = %v", err)
	}
}

func TestBuildProposalPropagatesDevelopmentTestAndFinalCancellation(t *testing.T) {
	judgments, searcher := rankingFixture()
	config := DefaultConfig("cancellation", FamilyLinearLambdaRank)
	datasets, err := retrieveCandidateDatasets(t.Context(), searcher, judgments)
	if err != nil {
		t.Fatalf("retrieveCandidateDatasets: %v", err)
	}
	split, err := splitCandidateDatasets(datasets, config.Split)
	if err != nil {
		t.Fatalf("splitCandidateDatasets: %v", err)
	}

	for _, test := range []struct {
		name     string
		target   string
		cancelAt int
	}{
		{
			name:     "development",
			target:   "rankingtrain.evaluateRankingModel",
			cancelAt: 1,
		},
		{
			name:     "test",
			target:   "rankingtrain.evaluateRankingModel",
			cancelAt: len(split.development) + 1,
		},
		{
			name:     "final",
			target:   "rankingtrain.BuildProposal",
			cancelAt: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, candidateSearcher := rankingFixture()
			ctx := &callerCancellationContext{
				target:   test.target,
				cancelAt: test.cancelAt,
			}
			if _, err := BuildProposal(ctx, candidateSearcher, judgments, config); !errors.Is(
				err,
				context.Canceled,
			) {
				t.Fatalf("cancellation error = %v, calls = %d", err, ctx.calls)
			}
		})
	}
}

type callerCancellationContext struct {
	target   string
	cancelAt int
	calls    int
}

func (*callerCancellationContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (*callerCancellationContext) Done() <-chan struct{} {
	return nil
}

func (c *callerCancellationContext) Err() error {
	programCounter, _, _, found := runtime.Caller(1)
	if !found {
		return nil
	}
	function := runtime.FuncForPC(programCounter)
	if function == nil || !strings.HasSuffix(function.Name(), c.target) {
		return nil
	}
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}

	return nil
}

func (*callerCancellationContext) Value(any) any {
	return nil
}
