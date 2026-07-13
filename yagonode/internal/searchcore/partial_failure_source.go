package searchcore

const (
	PartialFailureSourceRemoteYaCy      = "remote-yacy"
	PartialFailureSourceRemoteStage     = "remote-stage"
	PartialFailureSourcePeerReputation  = "peer-reputation"
	PartialFailureSourceExactStage      = "exact-stage"
	PartialFailureSourceLocalExactStage = "local-exact-stage"
	PartialFailureSourceFuzzyStage      = "fuzzy-stage"
	PartialFailureSourceLocalSearch     = "local-search"
	PartialFailureSourceWeb             = string(SourceWeb)
)
