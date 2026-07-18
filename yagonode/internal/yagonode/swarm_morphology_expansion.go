package yagonode

func prioritizedSwarmMorphologyForms(
	observed []string,
	generated []string,
) []string {
	forms := make([]string, 0, len(observed)+len(generated))
	seen := make(map[string]struct{}, cap(forms))
	generatedPosition := 0
	observedPosition := 0
	for generatedPosition < len(generated) || observedPosition < len(observed) {
		for range 2 {
			if generatedPosition >= len(generated) {
				break
			}
			forms = appendDistinctSwarmMorphologyForm(
				forms,
				seen,
				generated[generatedPosition],
			)
			generatedPosition++
		}
		if observedPosition < len(observed) {
			forms = appendDistinctSwarmMorphologyForm(
				forms,
				seen,
				observed[observedPosition],
			)
			observedPosition++
		}
	}

	return forms
}

func appendDistinctSwarmMorphologyForm(
	forms []string,
	seen map[string]struct{},
	form string,
) []string {
	if _, found := seen[form]; found {
		return forms
	}
	seen[form] = struct{}{}

	return append(forms, form)
}
