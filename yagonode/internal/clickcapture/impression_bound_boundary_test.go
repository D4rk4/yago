package clickcapture

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestIssuerAdmitsExactlyMaximumImpressionResultsAndRefusesOneMore(t *testing.T) {
	issuer, _ := issuerFixture(t)
	if _, err := issuer.Issue(
		"query",
		"model",
		compactDisplayedFixture(MaximumImpressionResults),
	); err != nil {
		t.Fatalf("impression at the result bound: %v", err)
	}
	_, err := issuer.Issue(
		"query",
		"model",
		compactDisplayedFixture(MaximumImpressionResults+1),
	)
	if err == nil || !strings.Contains(err.Error(), "impression result count is invalid") {
		t.Fatalf("impression above the result bound = %v", err)
	}
}

func TestIssuerAdmitsExactlyMaximumImpressionPositionAndRefusesOneMore(t *testing.T) {
	issuer, _ := issuerFixture(t)
	last := []DisplayedCandidate{candidateFixture(
		Candidate{
			URLIdentity:     "https://a.example/",
			ClusterIdentity: "cluster",
			Position:        MaximumImpressionPosition,
		},
		0.5,
		AttributionOriginal,
		0,
	)}
	token, err := issuer.Issue("query", "model", last)
	if err != nil {
		t.Fatalf("impression at the position bound: %v", err)
	}
	if _, err := issuer.ValidateClick(
		token,
		last[0].URLIdentity,
		MaximumImpressionPosition,
	); err != nil {
		t.Fatalf("click at the position bound: %v", err)
	}
	beyond := []DisplayedCandidate{candidateFixture(
		Candidate{
			URLIdentity:     "https://a.example/",
			ClusterIdentity: "cluster",
			Position:        MaximumImpressionPosition + 1,
		},
		0.5,
		AttributionOriginal,
		0,
	)}
	if _, err := issuer.Issue("query", "model", beyond); err == nil ||
		!strings.Contains(err.Error(), "impression position is invalid") {
		t.Fatalf("impression above the position bound = %v", err)
	}
}

func TestPropensityAtTheMeasurementFloorIsMeasuredAndBelowItIsRefused(t *testing.T) {
	issuer, _ := issuerFixture(t)
	floor := []DisplayedCandidate{candidateFixture(
		Candidate{URLIdentity: "https://a.example/", ClusterIdentity: "cluster", Position: 1},
		minimumMeasuredPropensity,
		AttributionOriginal,
		0,
	)}
	if _, err := issuer.Issue("query", "model", floor); err != nil {
		t.Fatalf("propensity at the measurement floor: %v", err)
	}
	if !measuredPropensity(minimumMeasuredPropensity) {
		t.Fatal("propensity at the measurement floor is not measured")
	}
	model := ModelEvidence{Assignment: "model", Results: map[string]ResultEvidence{}}
	addImpressionResults(&model, floor)
	result := model.Results["cluster"]
	if model.RandomizedImpressions != 1 || result.RandomizedImpressions != 1 ||
		result.ClippedExposureWeight != maximumInversePropensity {
		t.Fatalf("floor exposure evidence = %#v, result = %#v", model, result)
	}
	below := append([]DisplayedCandidate(nil), floor...)
	below[0].Propensity = math.Nextafter(minimumMeasuredPropensity, 0)
	if _, err := issuer.Issue("query", "model", below); err == nil ||
		!strings.Contains(err.Error(), "impression propensity is invalid") {
		t.Fatalf("propensity below the measurement floor = %v", err)
	}
}

func compactDisplayedFixture(count int) []DisplayedCandidate {
	results := make([]DisplayedCandidate, count)
	for index := range results {
		results[index] = candidateFixture(
			Candidate{
				URLIdentity:     fmt.Sprintf("https://example/%03d", index),
				ClusterIdentity: fmt.Sprintf("cluster-%03d", index),
				Position:        index + 1,
			},
			0.5,
			AttributionOriginal,
			min(index, MaximumImpressionResults-1),
		)
	}

	return results
}
