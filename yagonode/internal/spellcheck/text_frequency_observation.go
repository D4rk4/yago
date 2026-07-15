package spellcheck

func ObserveTextFrequencies(text string, synopses ...*FrequencySynopsis) {
	if !hasActiveFrequencySynopsis(synopses) {
		return
	}
	termsInText(text, func(term string) {
		for _, synopsis := range synopses {
			if synopsis == nil || synopsis.limit == 0 {
				continue
			}
			synopsis.observeTerm(term)
		}
	})
}

func hasActiveFrequencySynopsis(synopses []*FrequencySynopsis) bool {
	for _, synopsis := range synopses {
		if synopsis != nil && synopsis.limit > 0 {
			return true
		}
	}

	return false
}
