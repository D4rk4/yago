package cachedpage

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/snippetmark"
)

const (
	documentPassageAnalyzerParameter     = "analyzer"
	documentPassageTermsParameter        = "terms"
	documentPassageStartParameter        = "start"
	documentPassageEndParameter          = "end"
	maximumDocumentPassageAnalyzerBytes  = 64
	maximumDocumentPassageTermBytes      = 256
	maximumDocumentPassageTermsBytes     = 4 << 10
	maximumDocumentPassageTerms          = 32
	maximumDocumentPassageRangeBytes     = 8 << 10
	maximumDocumentPassageLinkRangeBytes = 1 << 10
	maximumDocumentPassageAbsoluteOffset = 1 << 30
	documentPassageSurroundingRunes      = 256
)

var documentPassagePage = template.Must(template.New("cached-passage").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="robots" content="noindex">
<title>Cached passage from {{.URL}}</title>
</head>
<body>
<main>
<p><strong>Cached passage.</strong> This analyzer-backed text range came from the copy stored by this node.
<a href="{{.FullCopyURL}}">Open the full cached copy</a>.</p>
<p><code>{{.URL}}</code></p>
<p>{{.Highlighted}}</p>
</main>
</body>
</html>`))

type documentPassageView struct {
	URL         string
	FullCopyURL string
	Highlighted template.HTML
}

type invalidDocumentPassageRequest interface {
	DocumentPassageRequestInvalid()
}

func URLForLocalResult(result searchcore.Result, terms []string) string {
	if !result.StoredLocally() {
		return ""
	}
	if passageURL := URLForPassage(
		result.DocumentID,
		result.Analyzer,
		terms,
		result.BodyQueryMatches,
	); passageURL != "" {
		return passageURL
	}

	return URLFor(result.URL)
}

func URLForPassage(
	documentID string,
	analyzer string,
	terms []string,
	matches []searchcore.QueryMatch,
) string {
	start, end, available := documentPassageLinkRange(matches)
	if !available {
		return ""
	}
	values := url.Values{
		"u":                              []string{documentID},
		documentPassageAnalyzerParameter: []string{analyzer},
		documentPassageTermsParameter:    append([]string(nil), terms...),
		documentPassageStartParameter:    []string{strconv.Itoa(start)},
		documentPassageEndParameter:      []string{strconv.Itoa(end)},
	}
	if _, err := parseDocumentPassageRequest(values, strings.TrimSpace(documentID)); err != nil {
		return ""
	}

	return Path + "?" + values.Encode()
}

func documentPassageLinkRange(matches []searchcore.QueryMatch) (int, int, bool) {
	valid := make([]searchcore.QueryMatch, 0, len(matches))
	for _, match := range matches {
		if match.Start < 0 || match.End <= match.Start ||
			match.End > maximumDocumentPassageAbsoluteOffset {
			continue
		}
		valid = append(valid, match)
	}
	if len(valid) == 0 {
		return 0, 0, false
	}
	sort.Slice(valid, func(left, right int) bool {
		if valid[left].Start != valid[right].Start {
			return valid[left].Start < valid[right].Start
		}

		return valid[left].End < valid[right].End
	})
	start := valid[0].Start
	end := valid[0].End
	for _, match := range valid[1:] {
		if match.End-start > maximumDocumentPassageLinkRangeBytes {
			break
		}
		end = max(end, match.End)
	}

	return start, end, true
}

func passageParametersPresent(values url.Values) bool {
	return values.Has(documentPassageAnalyzerParameter) ||
		values.Has(documentPassageTermsParameter) ||
		values.Has(documentPassageStartParameter) ||
		values.Has(documentPassageEndParameter)
}

func parseDocumentPassageRequest(
	values url.Values,
	documentID string,
) (searchcore.DocumentPassageRequest, error) {
	analyzers := values[documentPassageAnalyzerParameter]
	terms := values[documentPassageTermsParameter]
	starts := values[documentPassageStartParameter]
	ends := values[documentPassageEndParameter]
	if len(values["u"]) != 1 || len(analyzers) != 1 || len(starts) != 1 ||
		len(ends) != 1 || len(terms) == 0 || len(terms) > maximumDocumentPassageTerms {
		return searchcore.DocumentPassageRequest{}, errors.New("invalid passage parameters")
	}
	documentID = strings.TrimSpace(documentID)
	analyzer := strings.TrimSpace(analyzers[0])
	if documentID == "" || len(documentID) > maximumCachedPageURLBytes ||
		!utf8.ValidString(documentID) || !validDocumentPassageAnalyzer(analyzer) {
		return searchcore.DocumentPassageRequest{}, errors.New("invalid passage identity")
	}
	cleanedTerms := make([]string, len(terms))
	termBytes := 0
	for index, term := range terms {
		term = strings.TrimSpace(term)
		termBytes += len(term)
		if term == "" || len(term) > maximumDocumentPassageTermBytes ||
			termBytes > maximumDocumentPassageTermsBytes || !utf8.ValidString(term) {
			return searchcore.DocumentPassageRequest{}, errors.New("invalid passage terms")
		}
		cleanedTerms[index] = term
	}
	start, startErr := strconv.Atoi(starts[0])
	end, endErr := strconv.Atoi(ends[0])
	if startErr != nil || endErr != nil || start < 0 || end <= start ||
		end > maximumDocumentPassageAbsoluteOffset ||
		end-start > maximumDocumentPassageRangeBytes {
		return searchcore.DocumentPassageRequest{}, errors.New("invalid passage range")
	}

	return searchcore.DocumentPassageRequest{
		DocumentID:       documentID,
		Analyzer:         analyzer,
		Terms:            cleanedTerms,
		Start:            start,
		End:              end,
		SurroundingRunes: documentPassageSurroundingRunes,
	}, nil
}

func validDocumentPassageAnalyzer(analyzer string) bool {
	if analyzer == "" || len(analyzer) > maximumDocumentPassageAnalyzerBytes {
		return false
	}
	for _, current := range analyzer {
		if current >= 'a' && current <= 'z' || current >= '0' && current <= '9' ||
			current == '_' || current == '-' {
			continue
		}

		return false
	}

	return true
}

func (e endpoint) serveDocumentPassage(
	w http.ResponseWriter,
	r *http.Request,
	documentID string,
) {
	request, err := parseDocumentPassageRequest(r.URL.Query(), documentID)
	if err != nil {
		http.Error(w, "invalid cached passage request", http.StatusBadRequest)

		return
	}
	if e.passages == nil {
		http.Error(w, "cached passage unavailable", http.StatusNotFound)

		return
	}
	passage, found, err := e.passages.DocumentPassage(r.Context(), request)
	if err != nil {
		var invalid invalidDocumentPassageRequest
		if errors.As(err, &invalid) {
			http.Error(w, "invalid cached passage request", http.StatusBadRequest)

			return
		}
		http.Error(w, "cached passage unavailable", http.StatusInternalServerError)

		return
	}
	if !found {
		http.Error(w, "no cached passage of this page", http.StatusNotFound)

		return
	}
	matches := make([]snippetmark.QueryMatch, len(passage.QueryMatches))
	for index, match := range passage.QueryMatches {
		matches[index] = snippetmark.QueryMatch{Start: match.Start, End: match.End}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_ = documentPassagePage.Execute(w, documentPassageView{
		URL:         request.DocumentID,
		FullCopyURL: URLFor(request.DocumentID),
		Highlighted: snippetmark.HighlightMatches(passage.Text, request.Terms, matches),
	})
}
