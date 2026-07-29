package searchcore

func (f PartialFailure) SourceLabel() string {
	switch f.Source {
	case PartialFailureSourceWeb:
		return "web"
	case PartialFailureSourceQueryShape:
		// Every other source names a component an operator can go and look at.
		// This one names a property of the query, so the raw identifier would
		// read as a subsystem that does not exist.
		return "query without search words"
	}

	return f.Source
}
