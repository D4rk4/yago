package clickcapture

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestDeriveJudgmentsUsesClippedIPSAndSNIPSFloors(t *testing.T) {
	aggregates := []QueryEvidence{
		queryEvidenceFixture("zeta", 4, map[string]ResultEvidence{
			"top":  evidenceFixture("top", 4, 4, 2, evidenceWeights{8, 4}),
			"mid":  evidenceFixture("mid", 4, 4, 1, evidenceWeights{8, 2}),
			"weak": evidenceFixture("weak", 4, 4, 1, evidenceWeights{8, 1}),
			"none": evidenceFixture("none", 4, 4, 0, evidenceWeights{8, 0}),
		}),
		queryEvidenceFixture("below", 2, map[string]ResultEvidence{
			"one": evidenceFixture("one", 2, 2, 1, evidenceWeights{4, 2}),
		}),
		queryEvidenceFixture("alpha", 4, map[string]ResultEvidence{
			"zero":         evidenceFixture("zero", 4, 4, 0, evidenceWeights{8, 0}),
			"below-result": evidenceFixture("below-result", 4, 2, 1, evidenceWeights{4, 2}),
			"no-exposure":  evidenceFixture("no-exposure", 4, 4, 1, evidenceWeights{}),
		}),
	}
	aggregates[0].ObservedAtUnix = 1_800_000_000
	judgments := DeriveJudgments(aggregates, 3)
	if len(judgments) != 1 || judgments[0].Query != "zeta" {
		t.Fatalf("judgments = %#v", judgments)
	}
	relevant := judgments[0].Relevant
	if relevant["https://top/"] != gradeHighlyRelevant ||
		relevant["https://mid/"] != gradeHighlyRelevant ||
		relevant["https://weak/"] != gradeRelevant {
		t.Fatalf("relevant = %#v", relevant)
	}
	if _, present := relevant["https://none/"]; present {
		t.Fatal("nonclick result became relevant")
	}
	if !judgments[0].ObservedAt.Equal(time.Unix(1_800_000_000, 0).UTC()) {
		t.Fatalf("observed at = %v", judgments[0].ObservedAt)
	}
}

func TestDeriveJudgmentsClampsFloorCombinesModelsAndSorts(t *testing.T) {
	first := queryEvidenceFixture("zeta", 1, map[string]ResultEvidence{
		"cluster": evidenceWithURL(
			evidenceFixture("cluster", 1, 1, 1, evidenceWeights{2, 2}),
			"https://z/",
		),
	})
	secondModel := first.Models["model"]
	secondModel.Assignment = "second"
	secondModel.Results["cluster"] = evidenceWithURL(evidenceFixture(
		"cluster",
		1,
		1,
		1,
		evidenceWeights{2, 1},
	), "https://a/")
	first.Models["second"] = secondModel
	second := queryEvidenceFixture("alpha", 1, map[string]ResultEvidence{
		"alpha": evidenceFixture("alpha", 1, 1, 1, evidenceWeights{2, 2}),
	})
	judgments := DeriveJudgments([]QueryEvidence{first, second}, 0)
	if len(judgments) != 2 || judgments[0].Query != "alpha" || judgments[1].Query != "zeta" {
		t.Fatalf("judgments = %#v", judgments)
	}
	if judgments[1].Relevant["https://a/"] != gradeHighlyRelevant {
		t.Fatalf("combined representative = %#v", judgments[1].Relevant)
	}
}

func TestImplicitJudgmentsReadsStore(t *testing.T) {
	store := openClickStore(t)
	evidence := queryEvidenceFixture("query", 3, map[string]ResultEvidence{
		"cluster": evidenceWithURL(
			evidenceFixture("cluster", 3, 3, 1, evidenceWeights{6, 2}),
			"https://result/",
		),
	})
	if err := store.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return store.records.Put(tx, vault.Key(evidence.Query), evidence)
	}); err != nil {
		t.Fatalf("seed evidence: %v", err)
	}
	judgments, err := store.ImplicitJudgments(t.Context(), 3)
	if err != nil {
		t.Fatalf("ImplicitJudgments: %v", err)
	}
	if len(judgments) != 1 || judgments[0].Relevant["https://result/"] == 0 {
		t.Fatalf("judgments = %#v", judgments)
	}
}

func queryEvidenceFixture(
	query string,
	randomizedImpressions int,
	results map[string]ResultEvidence,
) QueryEvidence {
	evidence := newQueryEvidence(query)
	evidence.Models["model"] = ModelEvidence{
		Assignment:            "model",
		Impressions:           randomizedImpressions,
		RandomizedImpressions: randomizedImpressions,
		Results:               results,
	}

	return evidence
}

type evidenceWeights struct {
	exposure float64
	click    float64
}

func evidenceFixture(
	identity string,
	impressions int,
	randomizedImpressions int,
	clicks int,
	weights evidenceWeights,
) ResultEvidence {
	return ResultEvidence{
		URLIdentity:           "https://" + identity + "/",
		ClusterIdentity:       identity,
		Impressions:           impressions,
		RandomizedImpressions: randomizedImpressions,
		Clicks:                clicks,
		ClippedExposureWeight: weights.exposure,
		ClippedClickWeight:    weights.click,
	}
}

func evidenceWithURL(evidence ResultEvidence, url string) ResultEvidence {
	evidence.URLIdentity = url

	return evidence
}
