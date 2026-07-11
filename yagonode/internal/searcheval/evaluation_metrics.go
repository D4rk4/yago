package searcheval

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	relevanceCutoff       = 10
	firstRecallCutoff     = 100
	secondRecallCutoff    = 200
	alphaNovelty          = 0.5
	metricComparisonSlack = 1e-12
)

type QueryMetrics struct {
	ID                              string
	QueryCluster                    string
	SliceNames                      []string
	RecallAt100                     float64
	RecallAt200                     float64
	NDCGAt10                        float64
	ERRAt10                         float64
	Navigational                    bool
	NavigationalReciprocalRank      float64
	HasIntents                      bool
	AlphaNDCGAt10                   float64
	IntentCoverageAt10              float64
	DuplicateClusterRateAt10        float64
	UniqueRegistrableDomainCoverage float64
	UnsafeErrors                    int
	SpamErrors                      int
	UnsafeExposureAt10              float64
	SpamExposureAt10                float64
	PeerResourcesMeasured           bool
	PeerBytes                       int64
	PeerTimeouts                    int
	RerankLatency                   time.Duration
}

type MetricSet struct {
	Queries                         int
	RecallAt100                     float64
	RecallAt200                     float64
	NDCGAt10                        float64
	ERRAt10                         float64
	NavigationalMRR                 float64
	AlphaNDCGAt10                   float64
	IntentCoverageAt10              float64
	DuplicateClusterRateAt10        float64
	UniqueRegistrableDomainCoverage float64
	UnsafeErrors                    int
	SpamErrors                      int
	UnsafeExposureAt10              float64
	SpamExposureAt10                float64
	PeerResourcesMeasured           bool
	PeerBytes                       int64
	PeerTimeouts                    int
	RerankLatencyP50                time.Duration
	RerankLatencyP95                time.Duration
}

type EvaluationReport struct {
	Metrics MetricSet
	Slices  map[string]MetricSet
	Queries []QueryMetrics
}

func EvaluateHeldout(observations []QueryObservation) (EvaluationReport, error) {
	queries := make([]QueryMetrics, 0, len(observations))
	seen := make(map[string]bool, len(observations))
	for _, observation := range observations {
		if err := validateObservation(observation); err != nil {
			return EvaluationReport{}, err
		}
		id := observationID(observation)
		if seen[id] {
			return EvaluationReport{}, fmt.Errorf("duplicate observation id %q", id)
		}
		seen[id] = true
		queries = append(queries, measureQuery(id, observation))
	}
	sort.Slice(queries, func(i, j int) bool { return queries[i].ID < queries[j].ID })
	slices := make(map[string][]QueryMetrics)
	for _, query := range queries {
		for _, name := range query.SliceNames {
			slices[name] = append(slices[name], query)
		}
	}
	report := EvaluationReport{
		Metrics: metricSetFor(queries),
		Slices:  make(map[string]MetricSet, len(slices)),
		Queries: queries,
	}
	for name, sliceQueries := range slices {
		report.Slices[name] = metricSetFor(sliceQueries)
	}

	return report, nil
}

func validateObservation(observation QueryObservation) error {
	if observationID(observation) == "" {
		return fmt.Errorf("observation id and query cluster are empty")
	}
	if observation.PeerBytes < 0 || observation.PeerTimeouts < 0 ||
		observation.RerankLatency < 0 {
		return fmt.Errorf(
			"observation %q has negative resource measurements",
			observationID(observation),
		)
	}
	for _, candidate := range observation.Candidates {
		if candidateCluster(candidate) == "" {
			return fmt.Errorf(
				"observation %q has a candidate without identity",
				observationID(observation),
			)
		}
	}

	return nil
}

func measureQuery(id string, observation QueryObservation) QueryMetrics {
	judgment := observation.Judgment
	candidates := observation.Candidates
	metrics := QueryMetrics{
		ID:                              id,
		QueryCluster:                    judgmentCluster(judgment),
		SliceNames:                      normalizedSliceNames(judgment.SliceNames),
		RecallAt100:                     canonicalRecall(candidates, judgment, firstRecallCutoff),
		RecallAt200:                     canonicalRecall(candidates, judgment, secondRecallCutoff),
		NDCGAt10:                        canonicalNDCG(candidates, judgment, relevanceCutoff),
		ERRAt10:                         canonicalERR(candidates, judgment, relevanceCutoff),
		Navigational:                    judgment.Navigational,
		HasIntents:                      judgmentIntentCount(judgment) > 0,
		AlphaNDCGAt10:                   alphaNDCG(candidates, judgment, relevanceCutoff),
		IntentCoverageAt10:              intentCoverage(candidates, judgment, relevanceCutoff),
		DuplicateClusterRateAt10:        duplicateClusterRate(candidates, relevanceCutoff),
		UniqueRegistrableDomainCoverage: registrableDomainCoverage(candidates, relevanceCutoff),
		PeerResourcesMeasured:           observation.PeerResourcesMeasured,
		PeerBytes:                       observation.PeerBytes,
		PeerTimeouts:                    observation.PeerTimeouts,
		RerankLatency:                   observation.RerankLatency,
	}
	if judgment.Navigational {
		metrics.NavigationalReciprocalRank = canonicalReciprocalRank(
			candidates,
			judgment,
			secondRecallCutoff,
		)
	}
	metrics.UnsafeErrors, metrics.SpamErrors = safetyErrors(candidates, secondRecallCutoff)
	metrics.UnsafeExposureAt10 = discountedExposure(
		candidates,
		func(candidate RankedCandidate) bool { return candidate.Unsafe },
	)
	metrics.SpamExposureAt10 = discountedExposure(
		candidates,
		func(candidate RankedCandidate) bool { return candidate.Spam },
	)

	return metrics
}

func canonicalRecall(
	candidates []RankedCandidate,
	judgment CanonicalJudgment,
	cutoff int,
) float64 {
	relevant := relevantClusterCount(judgment)
	if relevant == 0 {
		return 0
	}
	found := make(map[string]bool)
	for _, candidate := range candidates[:min(len(candidates), cutoff)] {
		cluster := candidateCluster(candidate)
		if boundedGrade(judgment.RelevantClusters[cluster]) > 0 {
			found[cluster] = true
		}
	}

	return float64(len(found)) / float64(relevant)
}

func canonicalNDCG(
	candidates []RankedCandidate,
	judgment CanonicalJudgment,
	cutoff int,
) float64 {
	ideal := make([]int, 0, len(judgment.RelevantClusters))
	for cluster, grade := range judgment.RelevantClusters {
		if strings.TrimSpace(cluster) != "" && boundedGrade(grade) > 0 {
			ideal = append(ideal, boundedGrade(grade))
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ideal)))
	idcg := discountedSum(ideal, cutoff)
	if idcg == 0 {
		return 0
	}
	actual := make([]int, 0, min(len(candidates), cutoff))
	seen := make(map[string]bool)
	for _, candidate := range candidates[:min(len(candidates), cutoff)] {
		cluster := candidateCluster(candidate)
		grade := 0
		if !seen[cluster] {
			grade = candidateGrade(judgment, candidate)
			seen[cluster] = true
		}
		actual = append(actual, grade)
	}

	return discountedSum(actual, cutoff) / idcg
}

func canonicalERR(
	candidates []RankedCandidate,
	judgment CanonicalJudgment,
	cutoff int,
) float64 {
	maximum := 0
	for _, grade := range judgment.RelevantClusters {
		maximum = max(maximum, boundedGrade(grade))
	}
	if maximum == 0 {
		return 0
	}
	continuation := 1.0
	err := 0.0
	seen := make(map[string]bool)
	for index, candidate := range candidates[:min(len(candidates), cutoff)] {
		cluster := candidateCluster(candidate)
		grade := 0
		if !seen[cluster] {
			grade = candidateGrade(judgment, candidate)
			seen[cluster] = true
		}
		satisfaction := (math.Exp2(float64(grade)) - 1) / math.Exp2(float64(maximum))
		err += continuation * satisfaction / float64(index+1)
		continuation *= 1 - satisfaction
	}

	return err
}

func canonicalReciprocalRank(
	candidates []RankedCandidate,
	judgment CanonicalJudgment,
	cutoff int,
) float64 {
	for index, candidate := range candidates[:min(len(candidates), cutoff)] {
		if candidateGrade(judgment, candidate) > 0 {
			return 1 / float64(index+1)
		}
	}

	return 0
}

func alphaNDCG(
	candidates []RankedCandidate,
	judgment CanonicalJudgment,
	cutoff int,
) float64 {
	ideal := idealAlphaDCG(judgment, cutoff)
	if ideal == 0 {
		return 0
	}

	return alphaDCG(candidates, judgment, cutoff) / ideal
}

func alphaDCG(
	candidates []RankedCandidate,
	judgment CanonicalJudgment,
	cutoff int,
) float64 {
	covered := make(map[string]int)
	seen := make(map[string]bool)
	total := 0.0
	for index, candidate := range candidates[:min(len(candidates), cutoff)] {
		cluster := candidateCluster(candidate)
		gain := 0.0
		if !seen[cluster] {
			gain = alphaClusterGain(judgment, cluster, covered)
			for _, intent := range normalizedIntents(judgment.ClusterIntents[cluster]) {
				covered[intent]++
			}
			seen[cluster] = true
		}
		total += gain / math.Log2(float64(index)+2)
	}

	return total
}

func idealAlphaDCG(judgment CanonicalJudgment, cutoff int) float64 {
	remaining := make([]string, 0, len(judgment.RelevantClusters))
	for cluster, grade := range judgment.RelevantClusters {
		if boundedGrade(grade) > 0 && len(normalizedIntents(judgment.ClusterIntents[cluster])) > 0 {
			remaining = append(remaining, cluster)
		}
	}
	sort.Strings(remaining)
	covered := make(map[string]int)
	total := 0.0
	for rank := 0; rank < cutoff && len(remaining) > 0; rank++ {
		best := 0
		bestGain := alphaClusterGain(judgment, remaining[0], covered)
		for index := 1; index < len(remaining); index++ {
			gain := alphaClusterGain(judgment, remaining[index], covered)
			if gain > bestGain {
				best = index
				bestGain = gain
			}
		}
		cluster := remaining[best]
		total += bestGain / math.Log2(float64(rank)+2)
		for _, intent := range normalizedIntents(judgment.ClusterIntents[cluster]) {
			covered[intent]++
		}
		remaining = append(remaining[:best], remaining[best+1:]...)
	}

	return total
}

func alphaClusterGain(
	judgment CanonicalJudgment,
	cluster string,
	covered map[string]int,
) float64 {
	grade := boundedGrade(judgment.RelevantClusters[cluster])
	if grade == 0 {
		return 0
	}
	gain := 0.0
	for _, intent := range normalizedIntents(judgment.ClusterIntents[cluster]) {
		gain += math.Pow(1-alphaNovelty, float64(covered[intent]))
	}

	return (math.Exp2(float64(grade)) - 1) * gain
}

func intentCoverage(
	candidates []RankedCandidate,
	judgment CanonicalJudgment,
	cutoff int,
) float64 {
	total := judgmentIntents(judgment)
	if len(total) == 0 {
		return 0
	}
	covered := make(map[string]bool)
	seen := make(map[string]bool)
	for _, candidate := range candidates[:min(len(candidates), cutoff)] {
		cluster := candidateCluster(candidate)
		if seen[cluster] || candidateGrade(judgment, candidate) == 0 {
			continue
		}
		seen[cluster] = true
		for _, intent := range normalizedIntents(judgment.ClusterIntents[cluster]) {
			covered[intent] = true
		}
	}

	return float64(len(covered)) / float64(len(total))
}

func duplicateClusterRate(candidates []RankedCandidate, cutoff int) float64 {
	window := candidates[:min(len(candidates), cutoff)]
	if len(window) == 0 {
		return 0
	}
	seen := make(map[string]bool)
	duplicates := 0
	for _, candidate := range window {
		cluster := candidateCluster(candidate)
		if seen[cluster] {
			duplicates++
		} else {
			seen[cluster] = true
		}
	}

	return float64(duplicates) / float64(len(window))
}

func registrableDomainCoverage(candidates []RankedCandidate, cutoff int) float64 {
	window := candidates[:min(len(candidates), cutoff)]
	if len(window) == 0 {
		return 0
	}
	domains := make(map[string]bool)
	for _, candidate := range window {
		if domain := strings.ToLower(strings.TrimSpace(candidate.RegistrableDomain)); domain != "" {
			domains[domain] = true
		}
	}

	return float64(len(domains)) / float64(len(window))
}

func safetyErrors(candidates []RankedCandidate, cutoff int) (int, int) {
	unsafeErrors := 0
	spamErrors := 0
	for _, candidate := range candidates[:min(len(candidates), cutoff)] {
		if candidate.Unsafe {
			unsafeErrors++
		}
		if candidate.Spam {
			spamErrors++
		}
	}

	return unsafeErrors, spamErrors
}

func discountedExposure(
	candidates []RankedCandidate,
	exposed func(RankedCandidate) bool,
) float64 {
	window := candidates[:min(len(candidates), relevanceCutoff)]
	denominator := 0.0
	numerator := 0.0
	for index, candidate := range window {
		discount := 1 / math.Log2(float64(index)+2)
		denominator += discount
		if exposed(candidate) {
			numerator += discount
		}
	}
	if denominator == 0 {
		return 0
	}

	return numerator / denominator
}

func relevantClusterCount(judgment CanonicalJudgment) int {
	count := 0
	for cluster, grade := range judgment.RelevantClusters {
		if strings.TrimSpace(cluster) != "" && boundedGrade(grade) > 0 {
			count++
		}
	}

	return count
}

func judgmentIntents(judgment CanonicalJudgment) map[string]bool {
	intents := make(map[string]bool)
	for cluster, grade := range judgment.RelevantClusters {
		if boundedGrade(grade) == 0 {
			continue
		}
		for _, intent := range normalizedIntents(judgment.ClusterIntents[cluster]) {
			intents[intent] = true
		}
	}

	return intents
}

func judgmentIntentCount(judgment CanonicalJudgment) int {
	return len(judgmentIntents(judgment))
}

func normalizedIntents(intents []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(intents))
	for _, intent := range intents {
		intent = strings.Join(strings.Fields(strings.ToLower(intent)), " ")
		if intent == "" || seen[intent] {
			continue
		}
		seen[intent] = true
		out = append(out, intent)
	}
	sort.Strings(out)

	return out
}

func normalizedSliceNames(names []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.Join(strings.Fields(strings.ToLower(name)), " ")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)

	return out
}

func metricSetFor(queries []QueryMetrics) MetricSet {
	metrics := MetricSet{Queries: len(queries)}
	if len(queries) == 0 {
		return metrics
	}
	navigational := 0
	intentQueries := 0
	latencies := make([]time.Duration, 0, len(queries))
	metrics.PeerResourcesMeasured = true
	for _, query := range queries {
		metrics.RecallAt100 += query.RecallAt100
		metrics.RecallAt200 += query.RecallAt200
		metrics.NDCGAt10 += query.NDCGAt10
		metrics.ERRAt10 += query.ERRAt10
		metrics.DuplicateClusterRateAt10 += query.DuplicateClusterRateAt10
		metrics.UniqueRegistrableDomainCoverage += query.UniqueRegistrableDomainCoverage
		metrics.UnsafeErrors += query.UnsafeErrors
		metrics.SpamErrors += query.SpamErrors
		metrics.UnsafeExposureAt10 += query.UnsafeExposureAt10
		metrics.SpamExposureAt10 += query.SpamExposureAt10
		metrics.PeerResourcesMeasured = metrics.PeerResourcesMeasured &&
			query.PeerResourcesMeasured
		if query.PeerResourcesMeasured {
			metrics.PeerBytes += query.PeerBytes
			metrics.PeerTimeouts += query.PeerTimeouts
		}
		latencies = append(latencies, query.RerankLatency)
		if query.Navigational {
			metrics.NavigationalMRR += query.NavigationalReciprocalRank
			navigational++
		}
		if query.HasIntents {
			metrics.AlphaNDCGAt10 += query.AlphaNDCGAt10
			metrics.IntentCoverageAt10 += query.IntentCoverageAt10
			intentQueries++
		}
	}
	denominator := float64(len(queries))
	metrics.RecallAt100 /= denominator
	metrics.RecallAt200 /= denominator
	metrics.NDCGAt10 /= denominator
	metrics.ERRAt10 /= denominator
	metrics.DuplicateClusterRateAt10 /= denominator
	metrics.UniqueRegistrableDomainCoverage /= denominator
	metrics.UnsafeExposureAt10 /= denominator
	metrics.SpamExposureAt10 /= denominator
	if navigational > 0 {
		metrics.NavigationalMRR /= float64(navigational)
	}
	if intentQueries > 0 {
		metrics.AlphaNDCGAt10 /= float64(intentQueries)
		metrics.IntentCoverageAt10 /= float64(intentQueries)
	}
	metrics.RerankLatencyP50 = durationPercentile(latencies, 0.5)
	metrics.RerankLatencyP95 = durationPercentile(latencies, 0.95)

	return metrics
}

func durationPercentile(values []time.Duration, percentile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := max(0, int(math.Ceil(percentile*float64(len(sorted))))-1)

	return sorted[index]
}
