package clickcapture

type evidenceWeights struct {
	exposure float64
	click    float64
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

func evidenceFixture(
	impressions int,
	randomizedImpressions int,
	clicks int,
	weights evidenceWeights,
) ResultEvidence {
	return ResultEvidence{
		URLIdentity:           "https://cluster/",
		ClusterIdentity:       "cluster",
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
