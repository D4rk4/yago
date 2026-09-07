package hostrank

const (
	dampingFactor  = 0.85
	iterationCount = 40
)

type AuthorityEvidence struct {
	Score      float64
	Confidence float64
}

type AuthorityTable map[string]AuthorityEvidence

func (t AuthorityTable) Rank(domain string) float64 {
	return t[domain].Score
}

func (t AuthorityTable) Confidence(domain string) float64 {
	return t[domain].Confidence
}
