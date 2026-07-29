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
	// PartialFailureSourceQueryShape marks a stage that had nothing to ask
	// because of the shape of the caller's own query, not because a source was
	// lost. Nothing failed and no retry can change the outcome, so a failure
	// carrying this source is recorded for diagnosis but never counts towards
	// an answer the node cannot vouch for.
	PartialFailureSourceQueryShape = "query-shape"
)
