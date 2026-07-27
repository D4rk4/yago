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
		(len(r.PartialFailures) > 0 || !r.Availability.Exhausted)
}
