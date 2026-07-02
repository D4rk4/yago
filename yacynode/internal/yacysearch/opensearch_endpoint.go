package yacysearch

import (
	"encoding/xml"
	"net/http"
)

type openSearchEndpoint struct{}

type openSearchDescription struct {
	XMLName          xml.Name        `xml:"OpenSearchDescription"`
	XMLNS            string          `xml:"xmlns,attr"`
	XMLNSSuggestions string          `xml:"xmlns:suggestions,attr"`
	ShortName        string          `xml:"ShortName"`
	LongName         string          `xml:"LongName"`
	Images           []openSearchImg `xml:"Image"`
	Language         string          `xml:"Language"`
	OutputEncoding   string          `xml:"OutputEncoding"`
	InputEncoding    string          `xml:"InputEncoding"`
	AdultContent     string          `xml:"AdultContent"`
	Description      string          `xml:"Description"`
	URLs             []openSearchURL `xml:"Url"`
	Developer        string          `xml:"Developer"`
	Query            openSearchQuery `xml:"Query"`
	Tags             string          `xml:"Tags"`
	Contact          string          `xml:"Contact"`
	Attribution      string          `xml:"Attribution"`
	SyndicationRight string          `xml:"SyndicationRight"`
}

type openSearchImg struct {
	Type   string `xml:"type,attr,omitempty"`
	Width  string `xml:"width,attr,omitempty"`
	Height string `xml:"height,attr,omitempty"`
	Value  string `xml:",chardata"`
}

type openSearchURL struct {
	Type     string `xml:"type,attr"`
	Method   string `xml:"method,attr,omitempty"`
	Template string `xml:"template,attr"`
}

type openSearchQuery struct {
	Role        string `xml:"role,attr"`
	SearchTerms string `xml:"searchTerms,attr"`
}

func (openSearchEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/opensearchdescription+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	encoder := xml.NewEncoder(w)
	_ = encoder.EncodeToken(xml.ProcInst{
		Target: "xml",
		Inst:   []byte(`version="1.0" encoding="UTF-8"`),
	})
	_ = encoder.EncodeToken(xml.CharData("\n"))
	_ = encoder.Encode(openSearchDescriptionFor(r))
	_ = encoder.Flush()
}

func openSearchDescriptionFor(r *http.Request) openSearchDescription {
	base := requestBaseURL(r)

	return openSearchDescription{
		XMLNS:            "http://a9.com/-/spec/opensearch/1.1/",
		XMLNSSuggestions: "http://www.opensearch.org/specifications/opensearch/extensions/suggestions/1.1",
		ShortName:        "YaCy Search",
		LongName:         "YaCy P2P Search",
		Images: []openSearchImg{{
			Type:  "image/png",
			Value: base + "/env/grafics/yacy.png",
		}},
		Language:       "en-us",
		OutputEncoding: "UTF-8",
		InputEncoding:  "UTF-8",
		AdultContent:   "true",
		Description:    "YaCy-compatible peer search.",
		URLs: []openSearchURL{
			{
				Type:     "text/html",
				Method:   "GET",
				Template: base + "/yacysearch.html?query={searchTerms}&startRecord={startIndex?}&maximumRecords={count?}&nav=all&resource=global",
			},
			{
				Type:     "application/rss+xml",
				Method:   "GET",
				Template: base + "/yacysearch.rss?query={searchTerms}&startRecord={startIndex?}&maximumRecords={count?}&nav=all&resource=global",
			},
			{
				Type:     "application/x-suggestions+json",
				Template: base + "/suggest.json?query={searchTerms}",
			},
			{
				Type:     "application/x-suggestions+xml",
				Template: base + "/suggest.xml?query={searchTerms}",
			},
		},
		Developer:        "See https://github.com/D4rk4/yago",
		Query:            openSearchQuery{Role: "example", SearchTerms: "yacy free software"},
		Tags:             "YaCy Free Software Open Source P2P Peer-to-Peer Distributed Web Search Engine",
		Contact:          base + "/yacy/profile.html",
		Attribution:      "https://yacy.net YaCy Software; content: ask peer owner",
		SyndicationRight: "open",
	}
}
