package searchvisible

import "github.com/D4rk4/yago/yagonode/internal/searchindex"

type Text struct {
	Title   string
	Snippet string
	URL     string
}

func AnalyzerAvailable(name string) bool {
	return searchindex.StoredEvidenceAnalyzerAvailable(name)
}

func AnalyzerRequirementOrdinals(name string, requirements []string) ([]int, bool) {
	return searchindex.StoredEvidenceRequirementOrdinals(name, requirements)
}

func AnalyzerCompatible(name string, language string, text Text) bool {
	return searchindex.StoredEvidenceAnalyzerCompatible(
		name,
		language,
		searchindex.VisibleText{
			Title:   text.Title,
			Snippet: text.Snippet,
			URL:     text.URL,
		},
	)
}
