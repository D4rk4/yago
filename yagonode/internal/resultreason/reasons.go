package resultreason

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

const Maximum = 6

func For(result searchcore.Result) []string {
	reasons := make([]string, 0, Maximum)
	switch {
	case result.FromWeb():
		reasons = append(reasons, "Returned by the enabled web fallback.")
	case result.FromPeer():
		reasons = append(reasons, "Returned by one or more YaCy peers.")
	default:
		reasons = append(reasons, "Matched the local full-text index.")
	}
	reasons = appendPositive(
		reasons, result.Evidence, searchcore.SignalTitleScore, "The query matched the title.",
	)
	reasons = appendPositive(
		reasons, result.Evidence, searchcore.SignalHeadingScore, "The query matched a heading.",
	)
	reasons = appendPositive(
		reasons,
		result.Evidence,
		searchcore.SignalOrderedProximity,
		"The query words appear in order and close together.",
	)
	reasons = appendPositive(
		reasons,
		result.Evidence,
		searchcore.SignalAuthority,
		"Links from other indexed pages contributed authority.",
	)
	reasons = appendPositive(
		reasons,
		result.Evidence,
		searchcore.SignalFreshness,
		"Document freshness contributed to this rank.",
	)
	if support, known := result.Evidence.Value(
		searchcore.SignalSourceCount,
	); known && support > 1 &&
		len(reasons) < Maximum {
		reasons = append(reasons, "More than one retrieval source supported this result.")
	}

	return reasons
}

func appendPositive(
	reasons []string,
	evidence searchcore.RankingEvidence,
	signal searchcore.RankingSignal,
	reason string,
) []string {
	value, known := evidence.Value(signal)
	if !known || value <= 0 || len(reasons) >= Maximum {
		return reasons
	}

	return append(reasons, reason)
}
