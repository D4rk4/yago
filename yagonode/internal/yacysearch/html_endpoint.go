package yacysearch

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/cachedpage"
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
	JSONURL            string
	OpenSearchURL      string
	TotalResults       string
	Elapsed            string
	Recovered          bool
	DidYouMean         string
	DidYouMeanURL      string
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
	CachedURL   string
	Provenance  string
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
<a href="#results" style="position:absolute;left:-999px;top:0;background:#00f;color:#fff;padding:0.5rem">Skip to results</a>
<main>
<form method="get" action="{{.SearchURL}}">
<span style="position:relative;display:inline-block">
<input id="query" name="query" type="search" value="{{.Query}}" maxlength="200"
 role="combobox" aria-autocomplete="list" aria-expanded="false"
 aria-controls="ac-list" autocomplete="off" autofocus>
<ul id="ac-list" role="listbox" aria-label="Search suggestions" hidden
 style="position:absolute;left:0;right:0;top:100%;margin:0;padding:0;list-style:none;background:#fff;border:1px solid #ccc;z-index:10"></ul>
</span>
<input name="resource" type="hidden" value="{{.Resource}}">
<input name="contentdom" type="hidden" value="{{.ContentDomain}}">
<button type="submit">search</button>
</form>
<details>
<summary>Search operators</summary>
<ul>
<li><code>"exact phrase"</code> — match words adjacently</li>
<li><code>-word</code> — exclude a word</li>
<li><code>site:example.org</code> — one host only</li>
<li><code>inurl:blog</code> — URL must contain text</li>
<li><code>tld:de</code> — top-level domain</li>
<li><code>filetype:pdf</code> — document type</li>
<li><code>language:ru</code> — page language</li>
<li><code>/date</code> — newest results first</li>
</ul>
</details>
{{if .ShowResults}}
<section id="results" tabindex="-1">
<h1>Search for {{.Query}}</h1>
{{if .Recovered}}<p role="status">No exact matches — showing close matches instead.{{if .DidYouMean}} Did you mean <a href="{{.DidYouMeanURL}}">{{.DidYouMean}}</a>?{{end}}</p>{{end}}
<p role="status">{{.TotalResults}} results{{if .Elapsed}} ({{.Elapsed}}){{end}}</p>
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
<p>{{.DisplayURL}}{{if .Provenance}} [{{.Provenance}}]{{end}} {{.SizeName}} {{.Date}}{{if .CachedURL}} <a href="{{.CachedURL}}">cached</a>{{end}}</p>
</li>
{{end}}
</ol>
<p>Get these results as <a href="{{.RSSURL}}">RSS</a> · <a href="{{.JSONURL}}">JSON</a></p>
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
<script>
(function () {
  var input = document.getElementById("query");
  var list = document.getElementById("ac-list");
  if (!input || !list) return;
  var form = input.form, timer = null, options = [], active = -1;
  function close() {
    list.hidden = true; list.textContent = ""; options = []; active = -1;
    input.setAttribute("aria-expanded", "false");
    input.removeAttribute("aria-activedescendant");
  }
  function pick(i) { input.value = options[i].textContent; close(); form.submit(); }
  function highlight(i) {
    if (active >= 0) options[active].removeAttribute("aria-selected");
    active = i;
    if (i >= 0) {
      options[i].setAttribute("aria-selected", "true");
      input.setAttribute("aria-activedescendant", options[i].id);
    } else input.removeAttribute("aria-activedescendant");
  }
  function render(items) {
    close();
    if (!items.length) return;
    items.forEach(function (text, i) {
      var li = document.createElement("li");
      li.id = "ac-opt-" + i;
      li.setAttribute("role", "option");
      li.textContent = text;
      li.style.padding = "0.3rem 0.5rem";
      li.style.cursor = "pointer";
      li.addEventListener("mousedown", function (e) { e.preventDefault(); pick(i); });
      list.appendChild(li);
      options.push(li);
    });
    list.hidden = false;
    input.setAttribute("aria-expanded", "true");
  }
  input.addEventListener("input", function () {
    clearTimeout(timer);
    var q = input.value.trim();
    if (q.length < 2) { close(); return; }
    timer = setTimeout(function () {
      fetch("/suggest.json?q=" + encodeURIComponent(q))
        .then(function (r) { return r.json(); })
        .then(function (data) { render((data && data[1]) || []); })
        .catch(close);
    }, 200);
  });
  input.addEventListener("keydown", function (e) {
    if (list.hidden) return;
    if (e.key === "ArrowDown") { e.preventDefault(); highlight((active + 1) % options.length); }
    else if (e.key === "ArrowUp") { e.preventDefault(); highlight((active - 1 + options.length) % options.length); }
    else if (e.key === "Enter" && active >= 0) { e.preventDefault(); pick(active); }
    else if (e.key === "Escape") { close(); }
  });
  input.addEventListener("blur", function () { setTimeout(close, 120); });
})();
</script>
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
	started := htmlClock()
	resp, err := e.search.Search(r.Context(), req)
	if err != nil {
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}
	e.suggestions.Record(req.Query)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	page := responseHTML(r, resp)
	page.Elapsed = fmt.Sprintf("%.2f s", htmlClock().Sub(started).Seconds())
	page.NewTab = e.newTab
	_ = htmlSearchTemplate.Execute(w, page)
}

// htmlClock feeds the query-duration display; tests substitute a scripted clock.
var htmlClock = time.Now

func responseHTML(r *http.Request, resp searchcore.Response) htmlSearchPage {
	base := searchBaseURL(r)
	rawSearchURL := searchURL(base, resp.Request)

	page := htmlSearchPage{
		Query:              resp.Request.Query,
		Resource:           string(resp.Request.Source),
		ContentDomain:      string(resp.Request.ContentDomain),
		SearchURL:          searchBaseURL(r),
		RSSURL:             requestBaseURL(r) + "/yacysearch.rss" + rawSearchURL[len(base):],
		JSONURL:            requestBaseURL(r) + "/yacysearch.json" + rawSearchURL[len(base):],
		OpenSearchURL:      requestBaseURL(r) + "/opensearchdescription.xml",
		TotalResults:       strconv.Itoa(resp.TotalResults),
		Recovered:          resp.Recovered != "",
		DidYouMean:         resp.DidYouMean,
		Items:              responseHTMLItems(resp.Results, resp.Request.Terms),
		PartialFailures:    resp.PartialFailures,
		ShowResults:        resp.Request.Query != "",
		ShowPartialFailure: len(resp.PartialFailures) > 0,
	}
	if resp.DidYouMean != "" {
		values := url.Values{}
		values.Set(yagoproto.FieldQuery, resp.DidYouMean)
		page.DidYouMeanURL = base + "?" + values.Encode()
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
		item := htmlSearchItem{
			Title:      markWebResultTitle(result.Source, result.Title),
			URL:        result.URL,
			DisplayURL: result.DisplayURL,
			// Highlight escapes the snippet before adding <mark>, so this is
			// the only HTML the description may carry.
			Description: snippetmark.Highlight(result.Snippet, terms),
			Date:        result.DisplayDate(),
			SizeName:    sizeName(result.Size),
		}
		if result.StoredLocally() {
			// Only locally stored pages have a copy to show; local hits inside a
			// global search carry SourceGlobal, so this must not test SourceLocal.
			item.CachedURL = cachedpage.URLFor(result.URL)
		}
		if result.FromPeer() {
			item.Provenance = "peer"
		}
		items = append(items, item)
	}

	return items
}
