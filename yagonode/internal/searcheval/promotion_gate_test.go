package searcheval

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func promotionReport(values []float64) EvaluationReport {
	queries := make([]QueryMetrics, len(values))
	for index, value := range values {
		queries[index] = QueryMetrics{ID: string(rune('a' + index)), NDCGAt10: value}
	}
	metrics := MetricSet{RecallAt100: 0.8, RecallAt200: 0.9}
	slice := MetricSet{
		RecallAt100: 0.8, RecallAt200: 0.9, NDCGAt10: 0.6, ERRAt10: 0.5,
		NavigationalMRR: 0.4, AlphaNDCGAt10: 0.5, IntentCoverageAt10: 0.6,
		UniqueRegistrableDomainCoverage: 0.7, DuplicateClusterRateAt10: 0.1,
	}

	return EvaluationReport{
		Metrics: metrics,
		Slices:  map[string]MetricSet{"head": slice},
		Queries: queries,
	}
}

func TestDecideHeldoutPromotionPassesDeterministically(t *testing.T) {
	baseline := promotionReport([]float64{0.4, 0.5, 0.6, 0.7})
	candidate := promotionReport([]float64{0.42, 0.525, 0.63, 0.735})
	policy := PromotionPolicy{
		BootstrapSamples: 500, Confidence: 0.95, Seed: 9, MinimumHeldoutQueryClusters: 4,
	}
	first, err := DecideHeldoutPromotion(baseline, candidate, policy)
	if err != nil {
		t.Fatalf("DecideHeldoutPromotion: %v", err)
	}
	second, err := DecideHeldoutPromotion(baseline, candidate, policy)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("decision is not deterministic: %+v %+v err=%v", first, second, err)
	}
	if !first.Promote || len(first.Reasons) != 0 ||
		math.Abs(first.Confidence.ObservedRelativeGain-0.05) > 1e-12 ||
		first.Confidence.LowerRelativeGain <= 0 || first.Confidence.Samples != 500 {
		t.Fatalf("decision = %+v", first)
	}
	defaults := DefaultPromotionPolicy()
	if defaults.MinimumRelativeNDCGGain != 0.02 || defaults.BootstrapSamples != 2000 ||
		defaults.Confidence != 0.95 || defaults.Seed != 1 ||
		defaults.MinimumHeldoutQueryClusters != 20 ||
		defaults.MaximumRelativeLatencyIncrease != 0.2 ||
		defaults.LatencySlack != 2*time.Millisecond ||
		defaults.MaximumRelativePeerByteIncrease != 0.05 || defaults.PeerByteSlack != 1024 {
		t.Fatalf("defaults = %+v", defaults)
	}
	normalized := normalizedPromotionPolicy(PromotionPolicy{})
	if normalized.BootstrapSamples != defaults.BootstrapSamples ||
		normalized.Confidence != defaults.Confidence {
		t.Fatalf("normalized = %+v", normalized)
	}
}

func TestDecideHeldoutPromotionRejectsQualityAndSliceRegressions(t *testing.T) {
	baseline := promotionReport([]float64{0.5, 0.5, 0.5, 0.5})
	baseline.Metrics.CPULatencyP95 = 10 * time.Millisecond
	baseline.Metrics.PeerBytes = 1000
	baseline.Metrics.PeerTimeouts = 1
	below := promotionReport([]float64{0.505, 0.505, 0.505, 0.505})
	decision, err := DecideHeldoutPromotion(baseline, below, PromotionPolicy{
		BootstrapSamples: 100,
		Confidence:       0.9,
	})
	if err != nil || decision.Promote ||
		!strings.Contains(strings.Join(decision.Reasons, " "), "threshold") {
		t.Fatalf("below-threshold decision = %+v err=%v", decision, err)
	}

	mixed := promotionReport([]float64{0.7, 0.7, 0.35, 0.35})
	decision, err = DecideHeldoutPromotion(baseline, mixed, PromotionPolicy{
		BootstrapSamples: 1000,
		Confidence:       0.95,
		Seed:             4,
	})
	if err != nil || decision.Promote || decision.Confidence.LowerRelativeGain >= 0 {
		t.Fatalf("mixed decision = %+v err=%v", decision, err)
	}

	regressed := promotionReport([]float64{0.55, 0.55, 0.55, 0.55})
	regressed.Metrics.RecallAt100 = 0.7
	regressed.Metrics.RecallAt200 = 0.8
	regressed.Metrics.UnsafeExposureAt10 = 0.1
	regressed.Metrics.SpamExposureAt10 = 0.1
	regressed.Metrics.CPULatencyP95 = 20 * time.Millisecond
	regressed.Metrics.PeerBytes = 3000
	regressed.Metrics.PeerTimeouts = 2
	regressedSlice := regressed.Slices["head"]
	regressedSlice.NDCGAt10 = 0.5
	regressed.Slices["head"] = regressedSlice
	baseline.Slices["missing"] = baseline.Slices["head"]
	decision, err = DecideHeldoutPromotion(baseline, regressed, PromotionPolicy{
		BootstrapSamples: 100,
		Confidence:       0.9,
	})
	joined := strings.Join(decision.Reasons, " ")
	for _, marker := range []string{
		"recall@100", "recall@200", "unsafe", "spam", "latency", "peer bytes",
		"peer timeouts", "regressed", "missing",
	} {
		if !strings.Contains(joined, marker) {
			t.Fatalf("missing %q in reasons %v", marker, decision.Reasons)
		}
	}
	if err != nil || decision.Promote {
		t.Fatalf("regression decision = %+v err=%v", decision, err)
	}
}

func TestPromotionRequiresWinsOverLexicalAndActiveIncumbent(t *testing.T) {
	baseline := promotionReport([]float64{0.4, 0.4, 0.4, 0.4})
	incumbent := promotionReport([]float64{0.6, 0.6, 0.6, 0.6})
	candidate := promotionReport([]float64{0.55, 0.55, 0.55, 0.55})
	policy := PromotionPolicy{
		BootstrapSamples: 200, Confidence: 0.9, MinimumHeldoutQueryClusters: 4,
	}
	decision, err := DecideHeldoutPromotionWithIncumbent(
		baseline,
		incumbent,
		candidate,
		policy,
	)
	if err != nil || decision.Promote || decision.IncumbentConfidence == nil ||
		!strings.Contains(strings.Join(decision.Reasons, " "), "active incumbent") {
		t.Fatalf("incumbent rejection = %+v, %v", decision, err)
	}
	candidate = promotionReport([]float64{0.63, 0.63, 0.63, 0.63})
	decision, err = DecideHeldoutPromotionWithIncumbent(
		baseline,
		incumbent,
		candidate,
		policy,
	)
	if err != nil || !decision.Promote || decision.IncumbentConfidence == nil ||
		decision.IncumbentConfidence.LowerRelativeGain <= 0 {
		t.Fatalf("incumbent promotion = %+v, %v", decision, err)
	}
}

func TestPromotionUsesClusterBootstrapAndRankDiscountedSafety(t *testing.T) {
	baseline := promotionReport([]float64{0.4, 0.6, 0.5, 0.5})
	candidate := promotionReport([]float64{0.6, 0.4, 0.55, 0.55})
	for index, cluster := range []string{"paraphrase", "paraphrase", "second", "third"} {
		baseline.Queries[index].QueryCluster = cluster
		candidate.Queries[index].QueryCluster = cluster
	}
	confidence, err := PairedBootstrapNDCG(
		baseline.Queries,
		candidate.Queries,
		200,
		0.9,
		3,
	)
	if err != nil || confidence.QueryClusters != 3 ||
		math.Abs(confidence.ObservedRelativeGain-(1.6/3-0.5)/0.5) > 1e-12 {
		t.Fatalf("cluster confidence = %+v, %v", confidence, err)
	}
	decision, err := DecideHeldoutPromotion(baseline, candidate, PromotionPolicy{
		BootstrapSamples: 200, Confidence: 0.9, MinimumHeldoutQueryClusters: 4,
	})
	if err != nil || decision.Promote ||
		!strings.Contains(strings.Join(decision.Reasons, " "), "independent query clusters") {
		t.Fatalf("cluster minimum = %+v, %v", decision, err)
	}

	safeBaseline := promotionReport([]float64{0.5, 0.5, 0.5, 0.5})
	safeCandidate := promotionReport([]float64{0.55, 0.55, 0.55, 0.55})
	safeBaseline.Metrics.UnsafeErrors = 1
	safeCandidate.Metrics.UnsafeErrors = 1
	safeBaseline.Metrics.UnsafeExposureAt10 = 0.05
	safeCandidate.Metrics.UnsafeExposureAt10 = 0.2
	decision, err = DecideHeldoutPromotion(safeBaseline, safeCandidate, PromotionPolicy{
		BootstrapSamples: 200, Confidence: 0.9, MinimumHeldoutQueryClusters: 4,
	})
	if err != nil || decision.Promote ||
		!strings.Contains(strings.Join(decision.Reasons, " "), "top-10 exposure") {
		t.Fatalf("rank-sensitive safety = %+v, %v", decision, err)
	}
}

func TestPairedBootstrapValidation(t *testing.T) {
	validArray := [1]QueryMetrics{{ID: "a", NDCGAt10: 0.5}}
	valid := validArray[:]
	if _, err := PairedBootstrapNDCG(valid, valid, 0, 0.95, 1); err == nil {
		t.Fatal("zero samples accepted")
	}
	if _, err := PairedBootstrapNDCG(valid, valid, 10, 1, 1); err == nil {
		t.Fatal("invalid confidence accepted")
	}
	cases := []struct {
		baseline  []QueryMetrics
		candidate []QueryMetrics
	}{
		{nil, nil},
		{valid, append(valid, QueryMetrics{ID: "b", NDCGAt10: 0.5})},
		{valid, []QueryMetrics{{ID: "b", NDCGAt10: 0.5}}},
		{[]QueryMetrics{{ID: "", NDCGAt10: 0.5}}, valid},
		{[]QueryMetrics{{ID: "a", NDCGAt10: math.NaN()}}, valid},
		{valid, []QueryMetrics{{ID: "a", NDCGAt10: math.NaN()}}},
		{[]QueryMetrics{{ID: "a", NDCGAt10: 2}}, valid},
		{[]QueryMetrics{{ID: "a"}, {ID: "a"}}, []QueryMetrics{{ID: "a"}, {ID: "b"}}},
		{
			[]QueryMetrics{{ID: "a", QueryCluster: "one", NDCGAt10: 0.5}},
			[]QueryMetrics{{ID: "a", QueryCluster: "two", NDCGAt10: 0.5}},
		},
	}
	for _, tc := range cases {
		if _, err := PairedBootstrapNDCG(tc.baseline, tc.candidate, 10, 0.9, 1); err == nil {
			t.Fatalf("invalid pair accepted: %+v %+v", tc.baseline, tc.candidate)
		}
	}
}

func TestPromotionPolicyValidation(t *testing.T) {
	baseline := promotionReport([]float64{0.5})
	candidate := promotionReport([]float64{0.6})
	policies := []PromotionPolicy{
		{MinimumRelativeNDCGGain: math.NaN(), BootstrapSamples: 1, Confidence: 0.9},
		{MinimumRelativeNDCGGain: -1, BootstrapSamples: 1, Confidence: 0.9},
		{BootstrapSamples: -1, Confidence: 0.9},
		{BootstrapSamples: 1, Confidence: 1},
		{BootstrapSamples: 1, Confidence: 0.9, MaximumRelativeLatencyIncrease: math.NaN()},
		{BootstrapSamples: 1, Confidence: 0.9, MaximumRelativeLatencyIncrease: -1},
		{BootstrapSamples: 1, Confidence: 0.9, LatencySlack: -1},
		{BootstrapSamples: 1, Confidence: 0.9, MaximumRelativePeerByteIncrease: math.Inf(1)},
		{BootstrapSamples: 1, Confidence: 0.9, MaximumRelativePeerByteIncrease: -1},
		{BootstrapSamples: 1, Confidence: 0.9, PeerByteSlack: -1},
		{BootstrapSamples: 1, Confidence: 0.9, MinimumHeldoutQueryClusters: 1},
	}
	for _, policy := range policies {
		if _, err := DecideHeldoutPromotion(baseline, candidate, policy); err == nil {
			t.Fatalf("policy accepted: %+v", policy)
		}
	}
	if _, err := DecideHeldoutPromotion(baseline, EvaluationReport{}, PromotionPolicy{
		BootstrapSamples: 1,
		Confidence:       0.9,
	}); err == nil {
		t.Fatal("unpaired promotion reports accepted")
	}
	invalidBaseline := baseline
	invalidBaseline.Metrics.NDCGAt10 = math.NaN()
	if _, err := DecideHeldoutPromotion(invalidBaseline, candidate, PromotionPolicy{
		BootstrapSamples: 1,
		Confidence:       0.9,
	}); err == nil {
		t.Fatal("non-finite baseline metrics accepted")
	}
	invalidCandidate := candidate
	invalidCandidate.Metrics.Queries = -1
	if _, err := DecideHeldoutPromotion(baseline, invalidCandidate, PromotionPolicy{
		BootstrapSamples: 1,
		Confidence:       0.9,
	}); err == nil {
		t.Fatal("negative candidate metrics accepted")
	}
	invalidSlice := candidate
	invalidSlice.Slices = map[string]MetricSet{"bad": {RecallAt100: 2}}
	if _, err := DecideHeldoutPromotion(baseline, invalidSlice, PromotionPolicy{
		BootstrapSamples: 1,
		Confidence:       0.9,
	}); err == nil {
		t.Fatal("invalid slice metrics accepted")
	}
	invalidIncumbent := baseline
	invalidIncumbent.Metrics.NDCGAt10 = math.NaN()
	if _, err := DecideHeldoutPromotionWithIncumbent(
		baseline,
		invalidIncumbent,
		candidate,
		PromotionPolicy{BootstrapSamples: 1, Confidence: 0.9},
	); err == nil {
		t.Fatal("invalid incumbent metrics accepted")
	}
	mismatchedIncumbent := baseline
	mismatchedIncumbent.Queries = []QueryMetrics{{ID: "other", NDCGAt10: 0.5}}
	if _, err := DecideHeldoutPromotionWithIncumbent(
		baseline,
		mismatchedIncumbent,
		candidate,
		PromotionPolicy{BootstrapSamples: 1, Confidence: 0.9},
	); err == nil {
		t.Fatal("mismatched incumbent queries accepted")
	}
}

func TestPromotionMetricHelpers(t *testing.T) {
	if relativeNDCGGain(0, 0) != 0 || relativeNDCGGain(0, 0.1) != 1 {
		t.Fatal("zero-baseline relative gain failed")
	}
	if floatPercentile([]float64{1, 2, 3}, -1) != 1 ||
		floatPercentile([]float64{1, 2, 3}, 2) != 3 {
		t.Fatal("percentile bounds failed")
	}
	if durationRegressed(time.Second, 1200*time.Millisecond, 0.2, 0) ||
		!durationRegressed(time.Second, 1200*time.Millisecond+1, 0.2, 0) ||
		resourceRegressed(100, 120, 0.2, 0) || !resourceRegressed(100, 121, 0.2, 0) {
		t.Fatal("resource promotion boundaries failed")
	}
	base := promotionReport([]float64{0.5}).Slices["head"]
	mutations := []func(*MetricSet){
		func(value *MetricSet) { value.RecallAt100-- },
		func(value *MetricSet) { value.RecallAt200-- },
		func(value *MetricSet) { value.ERRAt10-- },
		func(value *MetricSet) { value.NavigationalMRR-- },
		func(value *MetricSet) { value.AlphaNDCGAt10-- },
		func(value *MetricSet) { value.IntentCoverageAt10-- },
		func(value *MetricSet) { value.UniqueRegistrableDomainCoverage-- },
		func(value *MetricSet) { value.DuplicateClusterRateAt10++ },
		func(value *MetricSet) { value.UnsafeExposureAt10 += 0.1 },
		func(value *MetricSet) { value.SpamExposureAt10 += 0.1 },
	}
	for _, mutate := range mutations {
		changed := base
		mutate(&changed)
		if !sliceRegressed(base, changed) {
			t.Fatalf("slice regression missed: %+v", changed)
		}
	}
}
