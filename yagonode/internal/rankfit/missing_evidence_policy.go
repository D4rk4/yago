package rankfit

type missingEvidencePolicy uint8

const (
	missingEvidenceAsObservedZero missingEvidencePolicy = iota + 1
	missingEvidenceNeutral
)

func (p missingEvidencePolicy) valid() bool {
	return p == missingEvidenceAsObservedZero || p == missingEvidenceNeutral
}
