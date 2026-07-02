package yacysearch

import (
	"encoding/xml"
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/searchcore"
)

type rssEndpoint struct {
	search      searchcore.Searcher
	suggestions *recentQueries
}

func (e rssEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := searchRequestFromValues(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := e.search.Search(r.Context(), req)
	if err != nil {
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}
	e.suggestions.Record(req.Query)

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	encoder := xml.NewEncoder(w)
	_ = encoder.EncodeToken(xml.ProcInst{
		Target: "xml",
		Inst:   []byte(`version="1.0" encoding="UTF-8"`),
	})
	_ = encoder.EncodeToken(xml.CharData("\n"))
	_ = encoder.EncodeToken(xml.ProcInst{
		Target: "xml-stylesheet",
		Inst:   []byte(`type='text/xsl' href='/yacysearch.xsl' version='1.0'`),
	})
	_ = encoder.EncodeToken(xml.CharData("\n"))
	_ = encoder.Encode(responseRSS(r, resp))
	_ = encoder.Flush()
}
