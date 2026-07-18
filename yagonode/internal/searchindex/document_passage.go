package searchindex

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	maximumDocumentPassageRunes            = 2048
	maximumDocumentPassageTerms            = 32
	maximumDocumentPassageSurroundingRunes = 512
)

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
	QueryMatches []TextQueryMatch
}

func documentPassage(
	ctx context.Context,
	doc documentstore.Document,
	req DocumentPassageRequest,
) (DocumentPassage, error) {
	if err := validateDocumentPassageRequest(doc.ExtractedText, req); err != nil {
		return DocumentPassage{}, err
	}
	start, requestedEnd := contextualDocumentPassageRange(
		doc.ExtractedText,
		req.Start,
		req.End,
		req.SurroundingRunes,
	)
	end := boundedDocumentPassageEnd(doc.ExtractedText, start, requestedEnd)
	text := strings.Clone(doc.ExtractedText[start:end])
	matches := NewAnalyzedQueryTerms(req.Terms, req.Analyzer).TextMatches(text)
	if err := ctx.Err(); err != nil {
		return DocumentPassage{}, fmt.Errorf("document passage: %w", err)
	}

	return DocumentPassage{
		Text:         text,
		Start:        start,
		End:          end,
		QueryMatches: matches,
	}, nil
}

func validateDocumentPassageRequest(text string, req DocumentPassageRequest) error {
	if strings.TrimSpace(req.DocumentID) == "" || !utf8.ValidString(req.DocumentID) {
		return invalidDocumentPassageRequest("document passage id required")
	}
	if len(req.Terms) == 0 {
		return invalidDocumentPassageRequest("document passage terms required")
	}
	if strings.TrimSpace(req.Analyzer) == "" ||
		!StoredEvidenceAnalyzerAvailable(req.Analyzer) {
		return invalidDocumentPassageRequest("document passage analyzer invalid")
	}
	if len(req.Terms) > maximumDocumentPassageTerms {
		return invalidDocumentPassageRequest(fmt.Sprintf(
			"document passage terms exceed %d",
			maximumDocumentPassageTerms,
		))
	}
	for _, term := range req.Terms {
		if strings.TrimSpace(term) == "" || !utf8.ValidString(term) {
			return invalidDocumentPassageRequest("document passage term invalid")
		}
	}
	if req.Start < 0 || req.End <= req.Start || req.End > len(text) {
		return invalidDocumentPassageRequest("document passage range invalid")
	}
	if req.SurroundingRunes < 0 ||
		req.SurroundingRunes > maximumDocumentPassageSurroundingRunes {
		return invalidDocumentPassageRequest("document passage context invalid")
	}
	if !utf8.ValidString(text[req.Start:req.End]) {
		return invalidDocumentPassageRequest("document passage range splits UTF-8 text")
	}

	return nil
}

func contextualDocumentPassageRange(
	text string,
	start int,
	end int,
	surroundingRunes int,
) (int, int) {
	if surroundingRunes == 0 {
		return start, end
	}
	remaining := maximumDocumentPassageRunes - utf8.RuneCountInString(text[start:end])
	leftRunes := min(surroundingRunes, max(0, remaining/2))
	for start > 0 && leftRunes > 0 {
		_, size := utf8.DecodeLastRuneInString(text[:start])
		start -= size
		leftRunes--
		remaining--
	}
	rightRunes := min(surroundingRunes, max(0, remaining))
	for end < len(text) && rightRunes > 0 {
		_, size := utf8.DecodeRuneInString(text[end:])
		end += size
		rightRunes--
	}

	return start, end
}

func boundedDocumentPassageEnd(text string, start int, requestedEnd int) int {
	end := start
	for runes := 0; end < requestedEnd && runes < maximumDocumentPassageRunes; runes++ {
		_, size := utf8.DecodeRuneInString(text[end:requestedEnd])
		end += size
	}

	return end
}
