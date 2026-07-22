package searchremote

func (s searcher) observeTermAbstractPeerLifecycle(outcomes []peerAbstractOutcome) {
	results := make([]peerSearchResult, len(outcomes))
	for position, outcome := range outcomes {
		responseErr := outcome.responseErr
		if responseErr == nil {
			responseErr = outcome.abstractErr
		}
		results[position] = peerSearchResult{
			peer: outcome.peer,
			err:  responseErr,
		}
	}
	s.lifecycle.observe(results)
}
