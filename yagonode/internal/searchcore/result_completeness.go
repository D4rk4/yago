package searchcore

// UnprovenZero reports that a response carries no results and cannot show the
// absence is real. A search whose sources all answered and found nothing is a
// truthful zero; a search that lost a source, or that never settled its
// availability because the session layer discarded an incomplete answer, is a
// zero the node cannot vouch for. The two are byte-identical on the wire --
// empty items, totalResults 0 -- so every surface needs one predicate to tell
// a caller which one it received.
func (r Response) UnprovenZero() bool {
	return len(r.Results) == 0 &&
		(r.LostSourceFailures() > 0 || !r.Availability.Exhausted)
}

// LostSourceFailures counts the partial failures that stand for a source the
// node could not get an answer from. A failure describing the caller's own
// query instead -- a stage that had nothing to ask because the query carried no
// words -- is diagnosis, not loss: counting it told the caller to retry a query
// whose outcome is fixed. Availability decisions read this, never the raw
// length.
func (r Response) LostSourceFailures() int {
	lost := 0
	for _, failure := range r.PartialFailures {
		if failure.Source != PartialFailureSourceQueryShape {
			lost++
		}
	}

	return lost
}
