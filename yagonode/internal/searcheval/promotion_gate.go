package searcheval

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type BootstrapConfidence struct {
	ObservedRelativeGain float64
	LowerRelativeGain    float64
	UpperRelativeGain    float64
	Confidence           float64
	Samples              int
	QueryClusters        int
}

type PromotionPolicy struct {
	MinimumRelativeNDCGGain         float64
	MinimumHeldoutQueryClusters     int
	MaximumRelativeLatencyIncrease  float64
	LatencySlack                    time.Duration
	MaximumRelativePeerByteIncrease float64
	PeerByteSlack                   int64
	BootstrapSamples                int
	Confidence                      float64
	Seed                            uint64
}

type PromotionDecision struct {
	Promote             bool
	Confidence          BootstrapConfidence
	IncumbentConfidence *BootstrapConfidence
	Reasons             []string
}

func DefaultPromotionPolicy() PromotionPolicy {
	return PromotionPolicy{
		MinimumRelativeNDCGGain:         0.02,
		MinimumHeldoutQueryClusters:     20,
		MaximumRelativeLatencyIncrease:  0.2,
		LatencySlack:                    2 * time.Millisecond,
		MaximumRelativePeerByteIncrease: 0.05,
		PeerByteSlack:                   1024,
		BootstrapSamples:                2000,
		Confidence:                      0.95,
		Seed:                            1,
	}
}

func PairedBootstrapNDCG(
	baseline []QueryMetrics,
	candidate []QueryMetrics,
	samples int,
	confidence float64,
	seed uint64,
) (BootstrapConfidence, error) {
	if samples <= 0 {
		return BootstrapConfidence{}, fmt.Errorf("bootstrap samples must be positive")
	}
	if math.IsNaN(confidence) || confidence <= 0 || confidence >= 1 {
		return BootstrapConfidence{}, fmt.Errorf("bootstrap confidence must be in (0,1)")
	}
	baselineValues, candidateValues, err := pairedNDCGValues(baseline, candidate)
	if err != nil {
		return BootstrapConfidence{}, err
	}
	observed := relativeNDCGGain(meanFloat64(baselineValues), meanFloat64(candidateValues))
	gains := make([]float64, samples)
	random := deterministicBootstrap{state: seed}
	for sample := range gains {
		baselineMean := 0.0
		candidateMean := 0.0
		for range baselineValues {
			index := random.index(len(baselineValues))
			baselineMean += baselineValues[index]
			candidateMean += candidateValues[index]
		}
		baselineMean /= float64(len(baselineValues))
		candidateMean /= float64(len(candidateValues))
		gains[sample] = relativeNDCGGain(baselineMean, candidateMean)
	}
	sort.Float64s(gains)
	tail := (1 - confidence) / 2

	return BootstrapConfidence{
		ObservedRelativeGain: observed,
		LowerRelativeGain:    floatPercentile(gains, tail),
		UpperRelativeGain:    floatPercentile(gains, 1-tail),
		Confidence:           confidence,
		Samples:              samples,
		QueryClusters:        len(baselineValues),
	}, nil
}

func DecideHeldoutPromotion(
	baseline EvaluationReport,
	candidate EvaluationReport,
	policy PromotionPolicy,
) (PromotionDecision, error) {
	return decideHeldoutPromotion(baseline, nil, candidate, policy)
}

func DecideHeldoutPromotionWithIncumbent(
	baseline EvaluationReport,
	incumbent EvaluationReport,
	candidate EvaluationReport,
	policy PromotionPolicy,
) (PromotionDecision, error) {
	return decideHeldoutPromotion(baseline, &incumbent, candidate, policy)
}

func decideHeldoutPromotion(
	baseline EvaluationReport,
	incumbent *EvaluationReport,
	candidate EvaluationReport,
	policy PromotionPolicy,
) (PromotionDecision, error) {
	policy = normalizedPromotionPolicy(policy)
	if err := validatePromotionPolicy(policy); err != nil {
		return PromotionDecision{}, err
	}
	if err := validatePromotionReport(baseline, "baseline"); err != nil {
		return PromotionDecision{}, err
	}
	if err := validatePromotionReport(candidate, "candidate"); err != nil {
		return PromotionDecision{}, err
	}
	if incumbent != nil {
		if err := validatePromotionReport(*incumbent, "active incumbent"); err != nil {
			return PromotionDecision{}, err
		}
	}
	confidence, reasons, err := promotionComparison(baseline, candidate, policy)
	if err != nil {
		return PromotionDecision{}, err
	}
	decision := PromotionDecision{Confidence: confidence, Reasons: reasons}
	if incumbent != nil {
		incumbentConfidence, incumbentReasons, comparisonErr := promotionComparison(
			*incumbent,
			candidate,
			policy,
		)
		if comparisonErr != nil {
			return PromotionDecision{}, comparisonErr
		}
		decision.IncumbentConfidence = &incumbentConfidence
		for _, reason := range incumbentReasons {
			decision.Reasons = append(decision.Reasons, "active incumbent: "+reason)
		}
	}
	decision.Promote = len(decision.Reasons) == 0

	return decision, nil
}

func promotionComparison(
	reference EvaluationReport,
	candidate EvaluationReport,
	policy PromotionPolicy,
) (BootstrapConfidence, []string, error) {
	confidence, err := PairedBootstrapNDCG(
		reference.Queries,
		candidate.Queries,
		policy.BootstrapSamples,
		policy.Confidence,
		policy.Seed,
	)
	if err != nil {
		return BootstrapConfidence{}, nil, err
	}
	reasons := make([]string, 0)
	if confidence.QueryClusters < policy.MinimumHeldoutQueryClusters {
		reasons = append(reasons, fmt.Sprintf(
			"held-out evidence has %d independent query clusters, requires %d",
			confidence.QueryClusters,
			policy.MinimumHeldoutQueryClusters,
		))
	}
	if confidence.ObservedRelativeGain+metricComparisonSlack < policy.MinimumRelativeNDCGGain {
		reasons = append(reasons, "held-out ndcg gain is below the promotion threshold")
	}
	if confidence.LowerRelativeGain < -metricComparisonSlack {
		reasons = append(reasons, "paired bootstrap confidence interval crosses zero")
	}
	if metricLower(candidate.Metrics.RecallAt100, reference.Metrics.RecallAt100) {
		reasons = append(reasons, "recall@100 regressed")
	}
	if metricLower(candidate.Metrics.RecallAt200, reference.Metrics.RecallAt200) {
		reasons = append(reasons, "recall@200 regressed")
	}
	if metricHigher(candidate.Metrics.UnsafeExposureAt10, reference.Metrics.UnsafeExposureAt10) {
		reasons = append(reasons, "unsafe-result top-10 exposure increased")
	}
	if metricHigher(candidate.Metrics.SpamExposureAt10, reference.Metrics.SpamExposureAt10) {
		reasons = append(reasons, "spam-result top-10 exposure increased")
	}
	if durationRegressed(
		reference.Metrics.CPULatencyP95,
		candidate.Metrics.CPULatencyP95,
		policy.MaximumRelativeLatencyIncrease,
		policy.LatencySlack,
	) {
		reasons = append(reasons, "cpu latency p95 exceeded the promotion budget")
	}
	if resourceRegressed(
		reference.Metrics.PeerBytes,
		candidate.Metrics.PeerBytes,
		policy.MaximumRelativePeerByteIncrease,
		policy.PeerByteSlack,
	) {
		reasons = append(reasons, "peer bytes exceeded the promotion budget")
	}
	if candidate.Metrics.PeerTimeouts > reference.Metrics.PeerTimeouts {
		reasons = append(reasons, "peer timeouts increased")
	}
	reasons = append(reasons, sliceRegressionReasons(reference.Slices, candidate.Slices)...)

	return confidence, reasons, nil
}

func normalizedPromotionPolicy(policy PromotionPolicy) PromotionPolicy {
	defaults := DefaultPromotionPolicy()
	if policy.MinimumRelativeNDCGGain == 0 {
		policy.MinimumRelativeNDCGGain = defaults.MinimumRelativeNDCGGain
	}
	if policy.MinimumHeldoutQueryClusters == 0 {
		policy.MinimumHeldoutQueryClusters = defaults.MinimumHeldoutQueryClusters
	}
	if policy.BootstrapSamples == 0 {
		policy.BootstrapSamples = defaults.BootstrapSamples
	}
	if policy.Confidence == 0 {
		policy.Confidence = defaults.Confidence
	}
	if policy.MaximumRelativeLatencyIncrease == 0 {
		policy.MaximumRelativeLatencyIncrease = defaults.MaximumRelativeLatencyIncrease
	}
	if policy.LatencySlack == 0 {
		policy.LatencySlack = defaults.LatencySlack
	}
	if policy.MaximumRelativePeerByteIncrease == 0 {
		policy.MaximumRelativePeerByteIncrease = defaults.MaximumRelativePeerByteIncrease
	}
	if policy.PeerByteSlack == 0 {
		policy.PeerByteSlack = defaults.PeerByteSlack
	}

	return policy
}

func validatePromotionPolicy(policy PromotionPolicy) error {
	if math.IsNaN(policy.MinimumRelativeNDCGGain) ||
		math.IsInf(policy.MinimumRelativeNDCGGain, 0) ||
		policy.MinimumRelativeNDCGGain < 0 {
		return fmt.Errorf("minimum relative ndcg gain must be finite and non-negative")
	}
	if policy.MinimumHeldoutQueryClusters < 2 ||
		policy.MinimumHeldoutQueryClusters > 10000 {
		return fmt.Errorf("minimum held-out query clusters must be between 2 and 10000")
	}
	if policy.BootstrapSamples <= 0 {
		return fmt.Errorf("bootstrap samples must be positive")
	}
	if math.IsNaN(policy.Confidence) || policy.Confidence <= 0 || policy.Confidence >= 1 {
		return fmt.Errorf("confidence must be in (0,1)")
	}
	if invalidNonNegativeRate(policy.MaximumRelativeLatencyIncrease) ||
		policy.LatencySlack < 0 {
		return fmt.Errorf("latency promotion budget must be finite and non-negative")
	}
	if invalidNonNegativeRate(policy.MaximumRelativePeerByteIncrease) ||
		policy.PeerByteSlack < 0 {
		return fmt.Errorf("peer byte promotion budget must be finite and non-negative")
	}

	return nil
}

func invalidNonNegativeRate(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0) || value < 0
}

func durationRegressed(
	baseline time.Duration,
	candidate time.Duration,
	maximumRelativeIncrease float64,
	slack time.Duration,
) bool {
	limit := float64(baseline)*(1+maximumRelativeIncrease) + float64(slack)

	return float64(candidate) > limit
}

func resourceRegressed(
	baseline int64,
	candidate int64,
	maximumRelativeIncrease float64,
	slack int64,
) bool {
	limit := float64(baseline)*(1+maximumRelativeIncrease) + float64(slack)

	return float64(candidate) > limit
}

func validatePromotionReport(report EvaluationReport, name string) error {
	if err := validatePromotionMetricSet(report.Metrics); err != nil {
		return fmt.Errorf("%s metrics: %w", name, err)
	}
	for slice, metrics := range report.Slices {
		if err := validatePromotionMetricSet(metrics); err != nil {
			return fmt.Errorf("%s slice %q: %w", name, slice, err)
		}
	}

	return nil
}

func validatePromotionMetricSet(metrics MetricSet) error {
	rates := []float64{
		metrics.RecallAt100,
		metrics.RecallAt200,
		metrics.NDCGAt10,
		metrics.ERRAt10,
		metrics.NavigationalMRR,
		metrics.AlphaNDCGAt10,
		metrics.IntentCoverageAt10,
		metrics.DuplicateClusterRateAt10,
		metrics.UniqueRegistrableDomainCoverage,
		metrics.UnsafeExposureAt10,
		metrics.SpamExposureAt10,
	}
	for _, rate := range rates {
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 1 {
			return fmt.Errorf("rate metric must be finite and in [0,1]")
		}
	}
	if metrics.Queries < 0 || metrics.UnsafeErrors < 0 || metrics.SpamErrors < 0 ||
		metrics.PeerBytes < 0 || metrics.PeerTimeouts < 0 ||
		metrics.CPULatencyP50 < 0 || metrics.CPULatencyP95 < 0 {
		return fmt.Errorf("count and latency metrics must be non-negative")
	}

	return nil
}

func pairedNDCGValues(
	baseline []QueryMetrics,
	candidate []QueryMetrics,
) ([]float64, []float64, error) {
	if len(baseline) == 0 || len(candidate) == 0 {
		return nil, nil, fmt.Errorf("paired bootstrap needs query metrics")
	}
	baselineByID, err := queryNDCGByID(baseline)
	if err != nil {
		return nil, nil, err
	}
	candidateByID, err := queryNDCGByID(candidate)
	if err != nil {
		return nil, nil, err
	}
	if len(baselineByID) != len(candidateByID) {
		return nil, nil, fmt.Errorf("paired reports have different query sets")
	}
	ids := make([]string, 0, len(baselineByID))
	for id := range baselineByID {
		if _, found := candidateByID[id]; !found {
			return nil, nil, fmt.Errorf("paired reports differ at query %q", id)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	type clusterValues struct {
		baseline  float64
		candidate float64
		queries   int
	}
	clusters := make(map[string]clusterValues)
	for _, id := range ids {
		baselineQuery := baselineByID[id]
		candidateQuery := candidateByID[id]
		baselineCluster := bootstrapQueryCluster(baselineQuery)
		candidateCluster := bootstrapQueryCluster(candidateQuery)
		if baselineCluster != candidateCluster {
			return nil, nil, fmt.Errorf("paired reports differ at query %q cluster", id)
		}
		values := clusters[baselineCluster]
		values.baseline += baselineQuery.NDCGAt10
		values.candidate += candidateQuery.NDCGAt10
		values.queries++
		clusters[baselineCluster] = values
	}
	clusterNames := make([]string, 0, len(clusters))
	for cluster := range clusters {
		clusterNames = append(clusterNames, cluster)
	}
	sort.Strings(clusterNames)
	baselineValues := make([]float64, len(clusterNames))
	candidateValues := make([]float64, len(clusterNames))
	for index, cluster := range clusterNames {
		values := clusters[cluster]
		baselineValues[index] = values.baseline / float64(values.queries)
		candidateValues[index] = values.candidate / float64(values.queries)
	}

	return baselineValues, candidateValues, nil
}

func queryNDCGByID(queries []QueryMetrics) (map[string]QueryMetrics, error) {
	values := make(map[string]QueryMetrics, len(queries))
	for _, query := range queries {
		if query.ID == "" {
			return nil, fmt.Errorf("query metric id is empty")
		}
		if math.IsNaN(query.NDCGAt10) || math.IsInf(query.NDCGAt10, 0) {
			return nil, fmt.Errorf("query %q ndcg is not finite", query.ID)
		}
		if query.NDCGAt10 < 0 || query.NDCGAt10 > 1 {
			return nil, fmt.Errorf("query %q ndcg is outside [0,1]", query.ID)
		}
		if _, found := values[query.ID]; found {
			return nil, fmt.Errorf("duplicate query metric id %q", query.ID)
		}
		values[query.ID] = query
	}

	return values, nil
}

func bootstrapQueryCluster(query QueryMetrics) string {
	cluster := strings.Join(strings.Fields(strings.ToLower(query.QueryCluster)), " ")
	if cluster != "" {
		return cluster
	}

	return query.ID
}

func relativeNDCGGain(baseline, candidate float64) float64 {
	if math.Abs(baseline) <= metricComparisonSlack {
		if candidate <= metricComparisonSlack {
			return 0
		}

		return 1
	}

	return (candidate - baseline) / math.Abs(baseline)
}

func meanFloat64(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}

	return total / float64(len(values))
}

func floatPercentile(sorted []float64, percentile float64) float64 {
	index := int(math.Floor(percentile * float64(len(sorted))))
	index = min(max(index, 0), len(sorted)-1)

	return sorted[index]
}

func metricLower(candidate, baseline float64) bool {
	return candidate+metricComparisonSlack < baseline
}

func metricHigher(candidate, baseline float64) bool {
	return candidate > baseline+metricComparisonSlack
}

func sliceRegressionReasons(
	baseline map[string]MetricSet,
	candidate map[string]MetricSet,
) []string {
	names := make([]string, 0, len(baseline))
	for name := range baseline {
		names = append(names, name)
	}
	sort.Strings(names)
	reasons := make([]string, 0)
	for _, name := range names {
		before := baseline[name]
		after, found := candidate[name]
		if !found {
			reasons = append(reasons, fmt.Sprintf("slice %q is missing", name))

			continue
		}
		if sliceRegressed(before, after) {
			reasons = append(reasons, fmt.Sprintf("slice %q regressed", name))
		}
	}

	return reasons
}

func sliceRegressed(baseline, candidate MetricSet) bool {
	return metricLower(candidate.NDCGAt10, baseline.NDCGAt10) ||
		metricLower(candidate.RecallAt100, baseline.RecallAt100) ||
		metricLower(candidate.RecallAt200, baseline.RecallAt200) ||
		metricLower(candidate.ERRAt10, baseline.ERRAt10) ||
		metricLower(candidate.NavigationalMRR, baseline.NavigationalMRR) ||
		metricLower(candidate.AlphaNDCGAt10, baseline.AlphaNDCGAt10) ||
		metricLower(candidate.IntentCoverageAt10, baseline.IntentCoverageAt10) ||
		metricLower(
			candidate.UniqueRegistrableDomainCoverage,
			baseline.UniqueRegistrableDomainCoverage,
		) ||
		metricHigher(candidate.DuplicateClusterRateAt10, baseline.DuplicateClusterRateAt10) ||
		metricHigher(candidate.UnsafeExposureAt10, baseline.UnsafeExposureAt10) ||
		metricHigher(candidate.SpamExposureAt10, baseline.SpamExposureAt10)
}

type deterministicBootstrap struct {
	state uint64
}

func (r *deterministicBootstrap) index(length int) int {
	unit := float64(r.next()>>11) / float64(uint64(1)<<53)

	return min(length-1, int(unit*float64(length)))
}

func (r *deterministicBootstrap) next() uint64 {
	r.state += 0x9e3779b97f4a7c15
	value := r.state
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb

	return value ^ (value >> 31)
}
