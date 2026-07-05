package yacysearch

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/snippetmark"
	"github.com/D4rk4/yago/yagoproto"
)

type htmlEndpoint struct {
	search      searchcore.Searcher
	suggestions *recentQueries
	newTab      bool
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
	Page               int
	HasPrev            bool
	HasNext            bool
	PrevURL            string
	NextURL            string
	NewTab             bool
}

type htmlSearchItem struct {
	Title       string
	URL         string
	DisplayURL  string
	Description template.HTML
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
<h2><a href="{{.URL}}"{{if $.NewTab}} target="_blank" rel="noopener noreferrer nofollow"{{else}} rel="noreferrer nofollow"{{end}}>{{.Title}}{{if $.NewTab}}<span aria-hidden="true"> ↗</span><span style="position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0)"> (opens in new tab)</span>{{end}}</a></h2>
<p>{{.Description}}</p>
<p>{{.DisplayURL}} {{.SizeName}} {{.Date}}</p>
</li>
{{end}}
</ol>
{{if or .HasPrev .HasNext}}
<nav aria-label="Result pages">
{{if .HasPrev}}<a rel="prev" href="{{.PrevURL}}">&lsaquo; Previous</a>{{end}}
<span>Page {{.Page}}</span>
{{if .HasNext}}<a rel="next" href="{{.NextURL}}">Next &rsaquo;</a>{{end}}
</nav>
{{end}}
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
	page := responseHTML(r, resp)
	page.NewTab = e.newTab
	_ = htmlSearchTemplate.Execute(w, page)
}

func responseHTML(r *http.Request, resp searchcore.Response) htmlSearchPage {
	base := searchBaseURL(r)
	rawSearchURL := searchURL(base, resp.Request)

	page := htmlSearchPage{
		Query:              resp.Request.Query,
		Resource:           string(resp.Request.Source),
		ContentDomain:      string(resp.Request.ContentDomain),
		SearchURL:          searchBaseURL(r),
		RSSURL:             requestBaseURL(r) + "/yacysearch.rss" + rawSearchURL[len(base):],
		OpenSearchURL:      requestBaseURL(r) + "/opensearchdescription.xml",
		TotalResults:       strconv.Itoa(resp.TotalResults),
		Items:              responseHTMLItems(resp.Results, resp.Request.Terms),
		PartialFailures:    resp.PartialFailures,
		ShowResults:        resp.Request.Query != "",
		ShowPartialFailure: len(resp.PartialFailures) > 0,
	}
	applyHTMLPagination(&page, base, resp)

	return page
}

// applyHTMLPagination fills the prev/next navigation from the request window and
// total, using YaCy's startRecord/maximumRecords parameters so links stay
// compatible with the OpenSearch paging contract. It no-ops when the limit is
// unset (no meaningful page size).
func applyHTMLPagination(page *htmlSearchPage, base string, resp searchcore.Response) {
	limit := resp.Request.Limit
	if limit <= 0 {
		return
	}
	offset := resp.Request.Offset
	page.Page = offset/limit + 1

	if offset > 0 {
		prev := offset - limit
		if prev < 0 {
			prev = 0
		}
		page.HasPrev = true
		page.PrevURL = htmlPageURL(base, resp.Request, prev)
	}
	if offset+len(resp.Results) < resp.TotalResults {
		page.HasNext = true
		page.NextURL = htmlPageURL(base, resp.Request, offset+limit)
	}
}

func htmlPageURL(base string, req searchcore.Request, offset int) string {
	values := url.Values{}
	values.Set(yagoproto.FieldQuery, req.Query)
	values.Set(yagoproto.FieldResource, string(req.Source))
	values.Set(yagoproto.FieldContentDom, string(req.ContentDomain))
	values.Set(yagoproto.FieldMaximumRecords, strconv.Itoa(req.Limit))
	values.Set(yagoproto.FieldStartRecord, strconv.Itoa(offset))

	return base + "?" + values.Encode()
}

func responseHTMLItems(results []searchcore.Result, terms []string) []htmlSearchItem {
	items := make([]htmlSearchItem, 0, len(results))
	for _, result := range results {
		items = append(items, htmlSearchItem{
			Title:      markWebResultTitle(result.Source, result.Title),
			URL:        result.URL,
			DisplayURL: result.DisplayURL,
			// Highlight escapes the snippet before adding <mark>, so this is
			// the only HTML the description may carry.
			Description: snippetmark.Highlight(result.Snippet, terms),
			Date:        result.Date,
			SizeName:    sizeName(result.Size),
		})
	}

	return items
}
