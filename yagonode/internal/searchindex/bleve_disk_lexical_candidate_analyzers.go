package searchindex

func distinctLexicalCandidateAnalyzers(
	text string,
	analyzers []string,
) []string {
	selected := make([]string, 0, len(analyzers))
	tokenStreams := make(map[string]struct{}, len(analyzers))
	for _, analyzer := range analyzers {
		if cjkDictionaryQueryTerm(analyzer, text) {
			selected = append(selected, analyzer)

			continue
		}
		tokenStream, available := analyzerTermsSignature(
			cjkQueryAnalyzer(analyzer),
			[]string{text},
		)
		if !available {
			selected = append(selected, analyzer)

			continue
		}
		if _, found := tokenStreams[tokenStream]; found {
			continue
		}
		tokenStreams[tokenStream] = struct{}{}
		selected = append(selected, analyzer)
	}

	return selected
}
