package yacysearch

import (
	"encoding/xml"
	"net/http"
)

const searchSuggestionNamespace = "http://schemas.microsoft.com/Search/2008/suggestions"

type suggestXMLEndpoint struct {
	index       indexSuggester
	suggestions *recentQueries
}

type searchSuggestionXML struct {
	XMLName xml.Name          `xml:"SearchSuggestion"`
	XMLNS   string            `xml:"xmlns,attr"`
	Query   string            `xml:"Query"`
	Section suggestionSection `xml:"Section"`
}

type suggestionSection struct {
	Items []suggestionItem `xml:"Item"`
}

type suggestionItem struct {
	Text string `xml:"Text"`
}

func (e suggestXMLEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	params := parseSuggestParams(r)
	// Upstream serves the XML suggestions with open CORS so remote search boxes
	// can read them; match that.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/x-suggestions+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	encoder := xml.NewEncoder(w)
	_ = encoder.EncodeToken(xml.ProcInst{Target: "xml", Inst: []byte(`version="1.0"`)})
	_ = encoder.EncodeToken(xml.CharData("\n"))
	_ = encoder.Encode(
		suggestionResponse(
			params.query,
			mergedSuggestions(r.Context(), e.index, e.suggestions, params),
		),
	)
	_ = encoder.Flush()
}

func suggestionResponse(query string, values []string) searchSuggestionXML {
	items := make([]suggestionItem, 0, len(values))
	for _, value := range values {
		items = append(items, suggestionItem{Text: value})
	}

	return searchSuggestionXML{
		XMLNS:   searchSuggestionNamespace,
		Query:   query,
		Section: suggestionSection{Items: items},
	}
}
