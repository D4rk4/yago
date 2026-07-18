package searchcore

import "context"

type DocumentPassageRequest struct {
	DocumentID       string
	Analyzer         string
	Terms            []string
	Start            int
	End              int
	SurroundingRunes int
}

type DocumentPassage struct {
	Text         string
	Start        int
	End          int
	QueryMatches []QueryMatch
}

type DocumentPassageSearcher interface {
	DocumentPassage(
		context.Context,
		DocumentPassageRequest,
	) (DocumentPassage, bool, error)
}
