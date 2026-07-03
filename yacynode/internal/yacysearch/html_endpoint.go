package yacysearch

import (
	"html/template"
	"net/http"
	"strconv"

	"github.com/D4rk4/yago/yacynode/internal/searchcore"
)

type htmlEndpoint struct {
	search      searchcore.Searcher
	suggestions *recentQueries
}

type htmlSearchPage struct {
	Query              string
	Resource           string
	ContentDomain      string
	SearchURL          string
	RSSURL             string
	OpenSearchURL      string
	TotalResults       string
	Items              []htmlSearchItem
	PartialFailures    []searchcore.PartialFailure
	ShowResults        bool
	ShowPartialFailure bool
}

type htmlSearchItem struct {
	Title       string
	URL         string
	DisplayURL  string
	Description string
	Date        string
	SizeName    string
}

var htmlSearchTemplate = template.Must(template.New("yacysearch").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{if .Query}}Search for {{.Query}}{{else}}YaCy Search{{end}}</title>
<link rel="search" type="application/opensearchdescription+xml" title="YaCy Search" href="{{.OpenSearchURL}}">
<link rel="alternate" type="application/rss+xml" title="Search for {{.Query}}" href="{{.RSSURL}}">
</head>
<body>
<main>
<form method="get" action="{{.SearchURL}}">
<input name="query" type="search" value="{{.Query}}" maxlength="200" autofocus>
<input name="resource" type="hidden" value="{{.Resource}}">
<input name="contentdom" type="hidden" value="{{.ContentDomain}}">
<button type="submit">search</button>
</form>
{{if .ShowResults}}
<section>
<h1>Search for {{.Query}}</h1>
<p>{{.TotalResults}} results</p>
{{if .ShowPartialFailure}}
<ul>
{{range .PartialFailures}}<li>{{.Source}}: {{.Reason}}</li>{{end}}
</ul>
{{end}}
<ol>
{{range .Items}}
<li>
<h2><a href="{{.URL}}">{{.Title}}</a></h2>
<p>{{.Description}}</p>
<p>{{.DisplayURL}} {{.SizeName}} {{.Date}}</p>
</li>
{{end}}
</ol>
</section>
{{end}}
</main>
</body>
</html>`))

func (e htmlEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = htmlSearchTemplate.Execute(w, responseHTML(r, resp))
}

func responseHTML(r *http.Request, resp searchcore.Response) htmlSearchPage {
	base := searchBaseURL(r)
	rawSearchURL := searchURL(base, resp.Request)

	return htmlSearchPage{
		Query:              resp.Request.Query,
		Resource:           string(resp.Request.Source),
		ContentDomain:      string(resp.Request.ContentDomain),
		SearchURL:          searchBaseURL(r),
		RSSURL:             requestBaseURL(r) + "/yacysearch.rss" + rawSearchURL[len(base):],
		OpenSearchURL:      requestBaseURL(r) + "/opensearchdescription.xml",
		TotalResults:       strconv.Itoa(resp.TotalResults),
		Items:              responseHTMLItems(resp.Results),
		PartialFailures:    resp.PartialFailures,
		ShowResults:        resp.Request.Query != "",
		ShowPartialFailure: len(resp.PartialFailures) > 0,
	}
}

func responseHTMLItems(results []searchcore.Result) []htmlSearchItem {
	items := make([]htmlSearchItem, 0, len(results))
	for _, result := range results {
		items = append(items, htmlSearchItem{
			Title:       markWebResultTitle(result.Source, result.Title),
			URL:         result.URL,
			DisplayURL:  result.DisplayURL,
			Description: result.Snippet,
			Date:        result.Date,
			SizeName:    sizeName(result.Size),
		})
	}

	return items
}
