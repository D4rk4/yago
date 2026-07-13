package searchcore

func (f PartialFailure) SourceLabel() string {
	if f.Source == PartialFailureSourceWeb {
		return "web"
	}

	return f.Source
}
