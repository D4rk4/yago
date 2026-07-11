package clickcapture

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestDeriveJudgmentsUsesConfidentFairPairPreferences(t *testing.T) {
	zeta := pairQueryEvidenceFixture("zeta", "first", map[string]FairPairEvidence{
		fairPairKey("other-top", "top"): pairEvidence(
			pairCandidate("other-top", "https://other-top/"),
			pairCandidate("top", "https://top/"), 20, 0, 20,
		),
		fairPairKey("mid", "other-mid"): pairEvidence(
			pairCandidate("mid", "https://mid/"),
			pairCandidate("other-mid", "https://other-mid/"), 20, 18, 2,
		),
		fairPairKey("other-weak", "weak"): pairEvidence(
			pairCandidate("other-weak", "https://other-weak/"),
			pairCandidate("weak", "https://weak/"), 20, 5, 15,
		),
		fairPairKey("none", "uncertain"): pairEvidence(
			pairCandidate("none", "https://none/"),
			pairCandidate("uncertain", "https://uncertain/"), 20, 9, 11,
		),
	})
	zeta.ObservedAtUnix = 1_800_000_000
	below := pairQueryEvidenceFixture("below", "first", map[string]FairPairEvidence{
		fairPairKey("a", "b"): pairEvidence(
			pairCandidate("a", "https://a/"), pairCandidate("b", "https://b/"), 2, 0, 2,
		),
	})
	judgments := DeriveJudgments([]QueryEvidence{zeta, below}, 3)
	if len(judgments) != 1 || judgments[0].Query != "zeta" {
		t.Fatalf("judgments = %#v", judgments)
	}
	relevant := judgments[0].Relevant
	if relevant["https://top/"] != gradeHighlyRelevant ||
		relevant["https://mid/"] != gradeHighlyRelevant ||
		relevant["https://weak/"] != gradeRelevant {
		t.Fatalf("relevant = %#v", relevant)
	}
	if _, present := relevant["https://uncertain/"]; present {
		t.Fatal("uncertain pair produced relevance")
	}
	if !judgments[0].ObservedAt.Equal(time.Unix(1_800_000_000, 0).UTC()) {
		t.Fatalf("observed at = %v", judgments[0].ObservedAt)
	}
}

func TestDeriveJudgmentsCombinesModelsClampsFloorAndSorts(t *testing.T) {
	zeta := pairQueryEvidenceFixture("zeta", "first", map[string]FairPairEvidence{
		fairPairKey("cluster", "loser"): pairEvidence(
			pairCandidate("cluster", "https://z/"),
			pairCandidate("loser", "https://loser/"), 20, 20, 0,
		),
	})
	second := pairQueryEvidenceFixture("zeta", "second", map[string]FairPairEvidence{
		fairPairKey("cluster", "other"): pairEvidence(
			pairCandidate("cluster", "https://a/"),
			pairCandidate("other", "https://other/"), 20, 20, 0,
		),
	})
	zeta.Models["second"] = second.Models["second"]
	alpha := pairQueryEvidenceFixture("alpha", "first", map[string]FairPairEvidence{
		fairPairKey("alpha", "other"): pairEvidence(
			pairCandidate("alpha", "https://alpha/"),
			pairCandidate("other", "https://other/"), 20, 20, 0,
		),
	})
	judgments := DeriveJudgments([]QueryEvidence{zeta, alpha}, 0)
	if len(judgments) != 2 || judgments[0].Query != "alpha" ||
		judgments[1].Query != "zeta" ||
		judgments[1].Relevant["https://a/"] != gradeHighlyRelevant {
		t.Fatalf("judgments = %#v", judgments)
	}
}

func TestImplicitJudgmentsReadsStore(t *testing.T) {
	store := openClickStore(t)
	evidence := pairQueryEvidenceFixture("query", "first", map[string]FairPairEvidence{
		fairPairKey("cluster", "other"): pairEvidence(
			pairCandidate("cluster", "https://result/"),
			pairCandidate("other", "https://other/"), 20, 20, 0,
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

func TestNonpositiveFairPairEstimateStaysNonrelevant(t *testing.T) {
	if relevant := gradedEstimateURLs([]resultEstimate{
		{url: "zero", score: 0},
		{url: "winner", score: 1},
	}, 1); len(relevant) != 1 || relevant["winner"] != gradeHighlyRelevant {
		t.Fatalf("graded estimates = %#v", relevant)
	}
}

func pairQueryEvidenceFixture(
	query string,
	assignment string,
	pairs map[string]FairPairEvidence,
) QueryEvidence {
	evidence := newQueryEvidence(query)
	evidence.Models[assignment] = ModelEvidence{
		Assignment:  assignment,
		Impressions: 20,
		Results:     map[string]ResultEvidence{},
		FairPairs:   pairs,
	}

	return evidence
}

func pairEvidence(
	first Candidate,
	second Candidate,
	impressions int,
	firstClicks int,
	secondClicks int,
) FairPairEvidence {
	return FairPairEvidence{
		FirstCluster: first.ClusterIdentity, FirstURL: first.URLIdentity,
		SecondCluster: second.ClusterIdentity, SecondURL: second.URLIdentity,
		Impressions: impressions, FirstClicks: firstClicks, SecondClicks: secondClicks,
	}
}

func pairCandidate(cluster, url string) Candidate {
	return Candidate{ClusterIdentity: cluster, URLIdentity: url}
}
