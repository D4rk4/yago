package searchcore

const (
	PartialFailureSourceRemoteYaCy      = "remote-yacy"
	PartialFailureSourceRemoteStage     = "remote-stage"
	PartialFailureSourcePeerReputation  = "peer-reputation"
	PartialFailureSourceExactStage      = "exact-stage"
	PartialFailureSourceLocalExactStage = "local-exact-stage"
	PartialFailureSourceFuzzyStage      = "fuzzy-stage"
	PartialFailureSourceLocalSearch     = "local-search"
	// PartialFailureSourceLocalEvidence marks snippet evidence the local index
	// could not assemble. It was an undeclared literal, so no consumer
	// recognised it: the peer-failure reporter fed it to the peer-hash parser
	// and swallowed the error, and the incomplete-answer allowlist ignored it.
	PartialFailureSourceLocalEvidence = "local-evidence"
	PartialFailureSourceWeb           = string(SourceWeb)
)
