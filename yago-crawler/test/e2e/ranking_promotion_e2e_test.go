//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"
)

func TestNodePromotesLearnedRankingModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	network := newNetwork(t, ctx)
	node := startNodeBroker(t, ctx, network.Name)
	session := adminLogin(t, ctx, node.opsURL)
	corpus := newRankingPromotionCorpus()
	applyRankingPromotionProfile(t, ctx, node.opsURL, session)
	profileHandle := createRankingPromotionProfile(
		t,
		ctx,
		node.opsURL,
		session,
		corpus.documents[0].url,
	)
	ingestRankingPromotionCorpus(
		t,
		ctx,
		node.crawlRPCAddress,
		profileHandle,
		corpus,
	)
	if documents := indexedDocuments(t, ctx, node.opsURL, session); documents != 66 {
		t.Fatalf("indexed documents = %d, want 66", documents)
	}

	query := corpus.queries[0]
	beforeExplain := rankingPromotionExplain(t, ctx, node.opsURL, session, query.query)
	assertRankingPromotionBaseline(t, beforeExplain, query)
	beforePublic := rankingPromotionPublicSearch(t, ctx, node.publicURL, query.query)
	beforeTop, err := rankingResponseTopURL(beforePublic)
	if err != nil || beforeTop != query.badURL {
		t.Fatalf("baseline public top = %q, want %q, err = %v", beforeTop, query.badURL, err)
	}

	storeRankingPromotionJudgments(t, ctx, node.opsURL, session, corpus)
	training := trainRankingPromotionModel(t, ctx, node.opsURL, session)
	assertRankingPromotionTraining(t, training)
	model := rankingPromotionModel(t, ctx, node.opsURL, session)
	assertRankingPromotionModel(t, model)

	afterExplain := rankingPromotionExplain(t, ctx, node.opsURL, session, query.query)
	assertLearnedRankingPromotion(t, afterExplain, query)
	afterPublic := rankingPromotionPublicSearch(t, ctx, node.publicURL, query.query)
	afterTop, err := rankingResponseTopURL(afterPublic)
	if err != nil || afterTop != query.goodURL {
		t.Fatalf("learned public top = %q, want %q, err = %v", afterTop, query.goodURL, err)
	}
}

func assertRankingPromotionBaseline(
	t *testing.T,
	response rankingPromotionExplainResponse,
	query rankingPromotionQuery,
) {
	t.Helper()
	if response.ModelRevision != "" || response.ModelKind != "" {
		t.Fatalf("fresh explain model = %q/%q", response.ModelRevision, response.ModelKind)
	}
	if len(response.Results) != 3 {
		t.Fatalf(
			"baseline explain results = %d, want 3: %+v",
			len(response.Results),
			response.Results,
		)
	}
	wantURLs := []string{query.badURL, query.middleURL, query.goodURL}
	for index, want := range wantURLs {
		result := response.Results[index]
		if result.URL != want || !result.QualityKnown || result.Learned != nil {
			t.Fatalf("baseline result %d = %+v, want URL %q", index, result, want)
		}
	}
	if response.Results[0].Quality >= response.Results[1].Quality ||
		response.Results[1].Quality >= response.Results[2].Quality {
		t.Fatalf("baseline qualities are not increasing: %+v", response.Results)
	}
}

func assertRankingPromotionTraining(
	t *testing.T,
	response rankingPromotionTrainingResponse,
) {
	t.Helper()
	wantDataset := rankingPromotionDataset{
		Train:       rankingPromotionPartition{Queries: 1, Candidates: 3, ModelExamples: 3},
		Development: rankingPromotionPartition{Queries: 1, Candidates: 3, ModelExamples: 3},
		Test:        rankingPromotionPartition{Queries: 20, Candidates: 60, ModelExamples: 60},
	}
	if response.Revision != rankingRevision || response.ModelKind != rankingModelKind ||
		!response.Promoted || response.Dataset != wantDataset {
		t.Fatalf("training identity/dataset = %+v", response)
	}
	if response.Training.PreferencePairs != 3 || response.Training.Iterations <= 0 {
		t.Fatalf("training report = %+v", response.Training)
	}
	if response.Development.Baseline.Queries != 1 ||
		response.Development.Candidate.Queries != 1 ||
		response.Development.Candidate.NDCGAt10 <= response.Development.Baseline.NDCGAt10 ||
		response.Test.Baseline.Queries != 20 ||
		response.Test.Candidate.Queries != 20 ||
		response.Test.Candidate.NDCGAt10 <= response.Test.Baseline.NDCGAt10 {
		t.Fatalf(
			"held-out comparisons = development %+v, test %+v",
			response.Development,
			response.Test,
		)
	}
	promotion := response.Promotion
	if !promotion.Promote || promotion.QueryClusters != 20 || promotion.Samples != 2000 ||
		promotion.Confidence != 0.95 || promotion.LowerRelativeGain <= 0 ||
		promotion.ComparedWithIncumbent || len(promotion.Reasons) != 0 {
		t.Fatalf("promotion decision = %+v", promotion)
	}
}

func assertRankingPromotionModel(t *testing.T, response rankingPromotionModelResponse) {
	t.Helper()
	current := response.Status.Current
	if !current.Active || current.Revision != rankingRevision ||
		current.ModelKind != rankingModelKind ||
		response.ActiveSnapshot.Format != "yago-learned-rank-snapshot-v1" ||
		response.ActiveSnapshot.Revision != rankingRevision ||
		response.ActiveSnapshot.ModelKind != rankingModelKind ||
		response.ActiveSnapshot.Model.Format != "yago-linear-lambdarank-v2" {
		t.Fatalf("active model = %+v", response)
	}
	qualityWeight, found := rankingModelQualityWeight(response)
	if !found || qualityWeight <= 0 {
		t.Fatalf("active model quality weight = %v, found = %v", qualityWeight, found)
	}
}

func assertLearnedRankingPromotion(
	t *testing.T,
	response rankingPromotionExplainResponse,
	query rankingPromotionQuery,
) {
	t.Helper()
	if response.ModelRevision != rankingRevision || response.ModelKind != rankingModelKind ||
		len(response.Results) != 3 || response.Results[0].URL != query.goodURL {
		t.Fatalf("learned explain identity/results = %+v", response)
	}
	learned := response.Results[0].Learned
	if learned == nil || learned.OriginalRank != 3 || learned.ModelRank != 1 ||
		learned.FinalRank != 1 {
		t.Fatalf("learned top explanation = %+v", learned)
	}
	for _, signal := range learned.Signals {
		if signal.Name == "quality" {
			if !signal.Used || signal.Weight <= 0 || signal.Contribution <= 0 {
				t.Fatalf("learned quality signal = %+v", signal)
			}

			return
		}
	}
	t.Fatal("learned explanation has no quality signal")
}
