package yacysearch

import (
	"fmt"
	"html/template"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/cachedpage"
	"github.com/D4rk4/yago/yagonode/internal/resultreason"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

type htmlEndpoint struct {
	search       searchcore.Searcher
	suggestions  *recentQueries
	newTab       bool
	clickCapture ImpressionRecorder
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
	PageLinks          []htmlPageLink
	HasPrev            bool
	HasNext            bool
	PrevURL            string
	NextURL            string
	NewTab             bool
	NavGroups          []htmlNavGroup
	ClickCapture       bool
	ImpressionToken    string
}

type htmlPageLink struct {
	Number  int
	URL     string
	Current bool
}

// htmlNavGroup is one navigator (Provider, Filetype, Language, …) rendered as a
// collapsible block of refine links on the human search surface.
type htmlNavGroup struct {
	Name     string
	Elements []htmlNavElement
}

// htmlNavElement is one navigator value; URL is empty for dimensions without a
// query operator (protocols, dates), which render as a plain labelled count.
type htmlNavElement struct {
	Name  string
	Count int
	URL   string
}

// htmlPagerWindow caps how many numbered page links the pager shows.
const htmlPagerWindow = 10

type htmlSearchItem struct {
	Title       string
	URL         string
	DisplayURL  string
	Description template.HTML
	Date        string
	SizeName    string
	CachedURL   string
	Provenance  string
	Rank        int
	Identity    string
	Reasons     []string
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
<li><code>"quoted phrase"</code> — prefer results where the words appear adjacently</li>
<li><code>-word</code> — exclude a word</li>
<li><code>site:example.org</code> — one host only</li>
<li><code>inurl:blog</code> — URL must contain text</li>
<li><code>tld:de</code> — top-level domain</li>
<li><code>filetype:pdf</code> — document type</li>
<li><code>language:ru</code> — page language</li>
<li><code>author:name</code> — page author metadata</li>
<li><code>near</code> — all words close together</li>
<li><code>/date</code> — newest results first</li>
</ul>
</details>
{{if .ShowResults}}
<section id="results" tabindex="-1">
<h1>Search for {{.Query}}</h1>
{{if .Recovered}}<p role="status">No exact matches — showing close matches instead.{{if .DidYouMean}} Did you mean <a href="{{.DidYouMeanURL}}">{{.DidYouMean}}</a>?{{end}}</p>{{else if .DidYouMean}}<p role="status">No results matched. Did you mean <a href="{{.DidYouMeanURL}}">{{.DidYouMean}}</a>?</p>{{end}}
<p role="status">{{.TotalResults}} results{{if .Elapsed}} ({{.Elapsed}}){{end}}</p>
{{if .NavGroups}}
<nav aria-label="Refine results">
{{range .NavGroups}}<details>
<summary>{{.Name}}</summary>
<ul>
{{range .Elements}}<li>{{if .URL}}<a href="{{.URL}}">{{.Name}}</a>{{else}}{{.Name}}{{end}} ({{.Count}})</li>
{{end}}</ul>
</details>
{{end}}</nav>
{{end}}
{{if .ShowPartialFailure}}
<ul>
{{range .PartialFailures}}<li>{{.SourceLabel}}: {{.Reason}}</li>{{end}}
</ul>
{{end}}
<ol{{if .ClickCapture}} data-t="{{.ImpressionToken}}"{{end}}>
{{range .Items}}
<li>
<h2><a href="{{.URL}}"{{if $.ClickCapture}} data-p="{{.Rank}}" data-i="{{.Identity}}"{{end}}{{if $.NewTab}} target="_blank" rel="noopener noreferrer nofollow"{{else}} rel="noreferrer nofollow"{{end}}>{{.Title}}{{if $.NewTab}}<span aria-hidden="true"> ↗</span><span style="position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0)"> (opens in new tab)</span>{{end}}</a></h2>
<p>{{.Description}}</p>
<p>{{.DisplayURL}}{{if .Provenance}} [{{.Provenance}}]{{end}} {{.SizeName}} {{.Date}}{{if .CachedURL}} <a href="{{.CachedURL}}">cached</a>{{end}}</p>
{{if .Reasons}}<details><summary>Why this result?</summary><ul>{{range .Reasons}}<li>{{.}}</li>{{end}}</ul></details>{{end}}
</li>
{{end}}
</ol>
<p>Get these results as <a href="{{.RSSURL}}">RSS</a> · <a href="{{.JSONURL}}">JSON</a></p>
{{if or .HasPrev .HasNext .PageLinks}}
<nav aria-label="Result pages">
{{if .HasPrev}}<a rel="prev" href="{{.PrevURL}}">&lsaquo; Previous</a>{{end}}
{{range .PageLinks}}{{if .Current}}<span aria-current="page">{{.Number}}</span>{{else}}<a href="{{.URL}}">{{.Number}}</a>{{end}}
{{end}}
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
{{if .ClickCapture}}<script>
(function () {
  var ol = document.querySelector("ol[data-t]");
	if (!ol || !navigator.sendBeacon) return;
	var token = ol.getAttribute("data-t");
  function beacon(a) {
    try {
      var body = new URLSearchParams();
		body.set("t", token);
		body.set("i", a.getAttribute("data-i") || "");
      body.set("p", a.getAttribute("data-p") || "");
      navigator.sendBeacon("/searchclick", body);
    } catch (e) {}
  }
  ol.querySelectorAll("a[data-p]").forEach(function (a) {
    a.addEventListener("click", function () { beacon(a); });
    a.addEventListener("auxclick", function (e) { if (e.button === 1) beacon(a); });
  });
})();
</script>{{end}}
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
	e.suggestions.Record(req.SubmittedText())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	page := responseHTMLWithImpression(r, resp, e.clickCapture)
	page.Elapsed = fmt.Sprintf("%.2f s", htmlClock().Sub(started).Seconds())
	page.NewTab = e.newTab
	_ = htmlSearchTemplate.Execute(w, page)
}

// htmlClock feeds the query-duration display; tests substitute a scripted clock.
var htmlClock = time.Now

func responseHTML(r *http.Request, resp searchcore.Response) htmlSearchPage {
	base := searchBaseURL(r)
	rawSearchURL := searchURL(base, resp.Request)
	query := resp.Request.SubmittedText()

	page := htmlSearchPage{
		Query:         query,
		Resource:      string(resp.Request.Source),
		ContentDomain: string(resp.Request.ContentDomain),
		SearchURL:     searchBaseURL(r),
		RSSURL:        requestBaseURL(r) + "/yacysearch.rss" + rawSearchURL[len(base):],
		JSONURL:       requestBaseURL(r) + "/yacysearch.json" + rawSearchURL[len(base):],
		OpenSearchURL: requestBaseURL(r) + "/opensearchdescription.xml",
		TotalResults:  strconv.Itoa(resp.TotalResults),
		Recovered:     resp.Recovered != "",
		DidYouMean:    resp.DidYouMean,
		Items: responseHTMLItems(
			resp.Results,
			resp.Request.Terms,
			resp.Request.Offset,
		),
		PartialFailures:    resp.PartialFailures,
		ShowResults:        query != "",
		ShowPartialFailure: len(resp.PartialFailures) > 0,
	}
	if resp.DidYouMean != "" {
		values := url.Values{}
		values.Set(yagoproto.FieldQuery, resp.DidYouMean)
		page.DidYouMeanURL = base + "?" + values.Encode()
	}
	applyHTMLPagination(&page, base, r.URL.Query(), resp)
	page.NavGroups = htmlNavGroups(base, r.URL.Query(), resp)

	return page
}

// htmlNavGroups renders the shared navigation model as refine links for the
// human search surface. A linkable value appends its query operator to the
// current query while preserving every other active filter and returning to the
// first page, so refining never drops the operator's neighbours (the wire
// surfaces keep the query-only refine URL that YaCy clients expect).
func htmlNavGroups(base string, params url.Values, resp searchcore.Response) []htmlNavGroup {
	query := resp.Request.SubmittedText()
	built := buildNavigation(query, resp.Facets)
	groups := make([]htmlNavGroup, 0, len(built))
	for _, group := range built {
		elements := make([]htmlNavElement, 0, len(group.elements))
		for _, element := range group.elements {
			nav := htmlNavElement{Name: element.name, Count: element.count}
			if element.modifier != "" {
				nav.URL = htmlRefineURL(base, params, query, element.modifier)
			}
			elements = append(elements, nav)
		}
		groups = append(groups, htmlNavGroup{Name: group.displayName, Elements: elements})
	}

	return groups
}

// htmlRefineURL appends a navigator's query operator to the current query,
// preserving the client's other parameters and resetting to the first page.
func htmlRefineURL(base string, params url.Values, query, modifier string) string {
	values := maps.Clone(params)
	if values == nil {
		values = url.Values{}
	}
	values.Set(yagoproto.FieldQuery, strings.TrimSpace(query+" "+modifier))
	values.Set(yagoproto.FieldStartRecord, "0")
	values.Del(yagoproto.FieldCount)

	return base + "?" + values.Encode()
}

// applyHTMLPagination fills the prev/next navigation from the request window and
// total, using YaCy's startRecord/maximumRecords parameters so links stay
// compatible with the OpenSearch paging contract. params carries the client's
// original query parameters so every active filter survives a page move. It
// no-ops when the limit is unset (no meaningful page size).
func applyHTMLPagination(
	page *htmlSearchPage,
	base string,
	params url.Values,
	resp searchcore.Response,
) {
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
		page.PrevURL = htmlPageURL(base, params, limit, prev)
	}
	if offset+len(resp.Results) < resp.TotalResults {
		page.HasNext = true
		page.NextURL = htmlPageURL(base, params, limit, offset+limit)
	}
	page.PageLinks = htmlNumberedPages(base, params, resp, page.Page, limit)
}

// htmlNumberedPages builds up to htmlPagerWindow numbered links around the
// current page over the honest pageable total.
func htmlNumberedPages(
	base string,
	params url.Values,
	resp searchcore.Response,
	current, limit int,
) []htmlPageLink {
	last := (resp.TotalResults + limit - 1) / limit
	if last <= 1 {
		return nil
	}
	start := current - htmlPagerWindow/2
	if start+htmlPagerWindow-1 > last {
		start = last - htmlPagerWindow + 1
	}
	if start < 1 {
		start = 1
	}
	links := make([]htmlPageLink, 0, htmlPagerWindow)
	for number := start; number <= last && len(links) < htmlPagerWindow; number++ {
		links = append(links, htmlPageLink{
			Number:  number,
			URL:     htmlPageURL(base, params, limit, (number-1)*limit),
			Current: number == current,
		})
	}

	return links
}

// htmlPageURL builds a pager link that carries the client's original query
// parameters forward — query, resource, contentdom, and every active filter
// (author, language, filetype, verify, prefer, filter, nav, …) — overriding only
// the page window so no filter is dropped when the operator moves between pages.
func htmlPageURL(base string, params url.Values, limit, offset int) string {
	values := maps.Clone(params)
	if values == nil {
		values = url.Values{}
	}
	values.Set(yagoproto.FieldMaximumRecords, strconv.Itoa(limit))
	values.Set(yagoproto.FieldStartRecord, strconv.Itoa(offset))
	// count is the OpenSearch alias for maximumRecords; drop it so the two
	// page-size names cannot disagree on the next request.
	values.Del(yagoproto.FieldCount)

	return base + "?" + values.Encode()
}

func responseHTMLItems(
	results []searchcore.Result,
	terms []string,
	offset int,
) []htmlSearchItem {
	items := make([]htmlSearchItem, 0, len(results))
	for index, result := range results {
		item := htmlSearchItem{
			Title:    result.Title,
			URL:      result.URL,
			Identity: result.URL,
			// Rank is the result's 1-based position across all pages, so click
			// capture debiases by the position the searcher actually examined.
			Rank:       offset + index + 1,
			DisplayURL: result.DisplayURL,
			// Highlight escapes the snippet before adding <mark>, so this is
			// the only HTML the description may carry.
			Description: highlightedResultSnippet(result, terms),
			Date:        result.DisplayDate(),
			SizeName:    sizeName(result.Size),
			Reasons:     resultreason.For(result),
		}
		if result.StoredLocally() {
			// Only locally stored pages have a copy to show; local hits inside a
			// global search carry SourceGlobal, so this must not test SourceLocal.
			item.CachedURL = cachedpage.URLForLocalResult(result, terms)
		}
		switch {
		case result.FromWeb():
			item.Provenance = "web"
		case result.FromPeer():
			item.Provenance = "peer"
		}
		items = append(items, item)
	}

	return items
}
