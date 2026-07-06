package publicportal

import (
	"encoding/json"
	"encoding/xml"
	"log/slog"
	"net/http"
	"strings"
)

const (
	osddContentType = "application/opensearchdescription+xml"
	suggestionsType = "application/x-suggestions+json"
	resultLinkType  = "text/html"
	osddNamespace   = "http://a9.com/-/spec/opensearch/1.1/"

	osddPath    = "/opensearch.xml"
	suggestPath = "/opensearch/suggest"
)

type openSearchDescription struct {
	XMLName       xml.Name        `xml:"OpenSearchDescription"`
	Namespace     string          `xml:"xmlns,attr"`
	ShortName     string          `xml:"ShortName"`
	Description   string          `xml:"Description"`
	InputEncoding string          `xml:"InputEncoding"`
	URLs          []openSearchURL `xml:"Url"`
	Attribution   string          `xml:"Attribution"`
}

type openSearchURL struct {
	Type     string `xml:"type,attr"`
	Method   string `xml:"method,attr"`
	Template string `xml:"template,attr"`
}

// OpenSearch serves the portal's OpenSearch description document and a
// privacy-preserving suggestions endpoint so a browser can add the portal as a
// search engine. It exposes only the public search surface and never records the
// query.
type OpenSearch struct {
	brand string
}

// NewOpenSearch builds the OpenSearch handler for the public portal.
func NewOpenSearch() *OpenSearch {
	return &OpenSearch{brand: brand}
}

// DescribePath is the route that serves the OpenSearch description document.
func (o *OpenSearch) DescribePath() string { return osddPath }

// SuggestPath is the route that serves OpenSearch suggestions.
func (o *OpenSearch) SuggestPath() string { return suggestPath }

// Describe serves the OpenSearch description document, building absolute URL
// templates from the request's own origin so the browser searches this node.
func (o *OpenSearch) Describe(w http.ResponseWriter, r *http.Request) {
	base := requestOrigin(r)
	doc := openSearchDescription{
		Namespace:     osddNamespace,
		ShortName:     o.brand,
		Description:   "Search the " + o.brand + " network",
		InputEncoding: "UTF-8",
		URLs: []openSearchURL{
			{Type: resultLinkType, Method: http.MethodGet, Template: base + "/?q={searchTerms}"},
			{
				Type:     suggestionsType,
				Method:   http.MethodGet,
				Template: base + suggestPath + "?q={searchTerms}",
			},
		},
		Attribution: o.brand + " — free software under the GNU AGPL v3.",
	}

	w.Header().Set("Content-Type", osddContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	encoder := xml.NewEncoder(w)
	_ = encoder.EncodeToken(
		xml.ProcInst{Target: "xml", Inst: []byte(`version="1.0" encoding="UTF-8"`)},
	)
	_ = encoder.EncodeToken(xml.CharData("\n"))
	_ = encoder.Encode(doc)
	_ = encoder.Flush()
}

// Suggest serves an OpenSearch suggestions array. The portal keeps no query
// history, so the completion list is always empty (SEC-05); the response echoes
// only the caller's own query term.
func (o *OpenSearch) Suggest(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	w.Header().Set("Content-Type", suggestionsType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := json.NewEncoder(w).Encode([]any{query, []string{}}); err != nil {
		slog.WarnContext(
			r.Context(),
			"opensearch suggestions encode failed",
			slog.Any("error", err),
		)
	}
}

func requestOrigin(r *http.Request) string {
	if configured := configuredBaseURL(); configured != "" {
		return configured
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}

	return scheme + "://" + r.Host
}
