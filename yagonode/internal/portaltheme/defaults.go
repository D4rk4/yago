package portaltheme

// DefaultBody returns the built-in Handlebars body a design editor seeds from
// and a "Default design" reset returns to. The bodies are a faithful port of
// the built-in Go portal template, so the editors start from the page the
// visitor already sees. Crawled-content fields render through auto-escaping
// {{...}} expressions; only the pre-escaped highlighted snippet and the
// operator's shared styles block use raw {{{...}}} interpolation.
func DefaultBody(page string) string {
	switch page {
	case PageSearch:
		return defaultSearchBody
	case PageResults:
		return defaultResultsBody
	case SharedStyles:
		return defaultStylesBody
	default:
		return ""
	}
}

const defaultHead = `<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{#if query}}{{query}} — {{/if}}{{brand}} search</title>
<link rel="search" type="application/opensearchdescription+xml" title="{{brand}} search" href="/opensearch.xml">
{{#if rssUrl}}<link rel="alternate" type="application/rss+xml" title="{{brand}}: {{query}}" href="{{rssUrl}}">{{/if}}
<style>{{{styles}}}</style>`

const defaultSearchForm = `    <div class="brand"><b>ya</b>go</div>
    <form class="search" method="get" action="/" role="search">
      <span class="ac-wrap">
        <input id="q" type="search" name="q" value="{{query}}" aria-label="Search query"
          role="combobox" aria-autocomplete="list" aria-expanded="false"
          aria-controls="ac-list" autocomplete="off" autofocus>
        <ul id="ac-list" role="listbox" aria-label="Search suggestions" hidden></ul>
      </span>
      <button type="submit">Search</button>
    </form>
    <details class="ophelp">
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
    </details>`

const defaultFoot = `  <div class="foot">{{brand}} — free software under the GNU AGPL v3.<br>
  Searches fan out to peers in the YaCy network, which see your query terms; results are labelled by where they came from (<span class="prov prov-local">local</span> this node's index, <span class="prov prov-peer">peer</span> another node, <span class="prov prov-ddgs">[ddgs]</span> an external provider that receives your query, whose pages may be queued for this node to crawl).</div>`

const defaultScripts = `<script>
(function () {
  var searchForm = document.querySelector("form.search");
  var serp = document.getElementById("serp");
  if (searchForm && serp) {
    searchForm.addEventListener("submit", function () {
      serp.setAttribute("aria-busy", "true");
      var html = '<p class="meta" role="status">Searching…</p>';
      for (var i = 0; i < 5; i++) {
        html += '<div class="skel"><div class="skel-line skel-title"></div>' +
          '<div class="skel-line"></div><div class="skel-line skel-short"></div></div>';
      }
      serp.innerHTML = html;
    });
  }
})();
(function () {
  var input = document.getElementById("q");
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
</script>`

const defaultSearchBody = `<!doctype html>
<html lang="en">
<head>
` + defaultHead + `
</head>
<body>
<a class="skip" href="#serp">Skip to results</a>
<div class="wrap">
  <div class="home">
` + defaultSearchForm + `
  </div>
  <div id="serp" tabindex="-1"></div>
` + defaultFoot + `
</div>
` + defaultScripts + `
</body>
</html>
`

const defaultResultsBody = `<!doctype html>
<html lang="en">
<head>
` + defaultHead + `
</head>
<body>
<a class="skip" href="#serp">Skip to results</a>
<div class="wrap">
  <div class="topbar">
` + defaultSearchForm + `
  </div>
  {{#if verticals}}<nav class="tabs" aria-label="Content verticals">{{#each verticals}}{{#if current}}<span aria-current="page">{{label}}</span>{{else}}<a href="{{url}}">{{label}}</a>{{/if}}{{/each}}</nav>{{/if}}
  <div id="serp" tabindex="-1">
  {{#if error}}
  <p class="meta" role="alert">{{error}}</p>
  {{else}}{{#if submitted}}
  {{#if results.recovered}}
  <p class="meta" role="status">No exact matches for “{{results.query}}” — showing close matches instead.{{#if results.didYouMean}} Did you mean <a href="{{results.didYouMeanUrl}}">{{results.didYouMean}}</a>?{{/if}}</p>
  {{else}}{{#if results.didYouMean}}
  <p class="meta" role="status">No results matched. Did you mean <a href="{{results.didYouMeanUrl}}">{{results.didYouMean}}</a>?</p>
  {{/if}}
  {{/if}}
  <p class="meta" role="status">{{formatNumber results.totalResults}} {{pluralize results.totalResults "result" "results"}} for “{{results.query}}”{{#if elapsed}} ({{elapsed}}){{/if}}.{{#if results.results}} On this page: {{results.localCount}} from this node · {{results.peerCount}} from peers · {{results.webCount}} from DDGS.{{/if}}{{#if results.peersFailed}} {{results.peersFailed}} peer(s) unreachable or timed out.{{/if}}</p>
  {{#if results.facets}}<div class="serp-grid"><aside class="facets" aria-label="Filter results">
  <details open>
  <summary>Filters</summary>
  {{#each results.facets}}
  <fieldset><legend>{{title}}</legend>
  <ul>
  {{#each items}}<li>{{#if url}}<a href="{{url}}">{{label}}</a>{{else}}{{label}}{{/if}} <span class="count">({{count}})</span></li>
  {{/each}}
  </ul>
  </fieldset>
  {{/each}}
  </details>
  </aside><div>{{/if}}
  {{#if imageVertical}}
  <ul class="imggrid">
  {{#each results.results}}{{#each images}}
  <li><figure>
    <a href="#img-{{@../index}}-{{@index}}"><img src="{{proxyUrl}}" alt="{{alt}}" loading="lazy"></a>
    <figcaption>{{#if alt}}{{alt}}{{else}}{{../title}}{{/if}}</figcaption>
  </figure>
  <div class="lightbox" id="img-{{@../index}}-{{@index}}">
    <a class="close" href="#serp" aria-label="Close">×</a>
    <img src="{{proxyUrl}}" alt="{{alt}}">
    <p class="from">From <a href="{{../url}}" rel="noreferrer nofollow">{{../title}}</a></p>
  </div>
  </li>
  {{/each}}{{/each}}
  </ul>
  {{else}}
  {{#if results.results}}<ul class="results">{{/if}}
  {{#each results.results}}
  <li class="result">
    {{#if faviconUrl}}<img class="fav" src="{{faviconUrl}}" alt="" width="16" height="16" loading="lazy">{{/if}}
    <a class="title" href="{{url}}"{{#if ../newTab}} target="_blank" rel="noopener noreferrer nofollow"{{else}} rel="noreferrer nofollow"{{/if}}>{{#if title}}{{title}}{{else}}{{url}}{{/if}}{{#if ../newTab}}<span aria-hidden="true"> ↗</span><span class="sr-only"> (opens in new tab)</span>{{/if}}</a>
    <div class="url">{{#if displayUrl}}{{displayUrl}}{{else}}{{url}}{{/if}}{{#if provenance}} · <span class="prov prov-{{provenance}}">{{provenanceLabel}}</span>{{/if}}{{#if date}} · {{date}}{{/if}}{{#if sizeName}} · {{sizeName}}{{/if}}{{#if cachedUrl}} · <a href="{{cachedUrl}}">cached</a>{{/if}}</div>
    {{#if snippetHtml}}<p>{{{snippetHtml}}}</p>{{else}}{{#if snippet}}<p>{{snippet}}</p>{{/if}}{{/if}}
  </li>
  {{else}}
  <p class="meta" role="status">Nothing found.</p>
  {{/each}}
  {{#if results.results}}</ul>{{/if}}
  {{/if}}
  {{#if results.facets}}</div></div>{{/if}}
  {{#if pagination.show}}
  <nav class="pager" aria-label="Result pages">
    {{#if pagination.hasPrev}}<a rel="prev" href="{{pagination.prevUrl}}">‹ Previous</a>{{/if}}
    {{#each pagination.pages}}{{#if current}}<span class="page" aria-current="page">{{number}}</span>{{else}}<a href="{{url}}">{{number}}</a>{{/if}}
    {{/each}}
    {{#if pagination.hasNext}}<a rel="next" href="{{pagination.nextUrl}}">Next ›</a>{{/if}}
  </nav>
  {{/if}}
  {{#if rssUrl}}
  <p class="meta">Get these results as <a href="{{rssUrl}}">RSS</a> · <a href="{{jsonUrl}}">JSON</a> — or add this engine to your browser via <a href="/opensearch.xml">OpenSearch</a>.</p>
  {{/if}}
  {{/if}}{{/if}}
  </div>
` + defaultFoot + `
</div>
` + defaultScripts + `
</body>
</html>
`

const defaultStylesBody = `body { font-family: Arial, Helvetica, sans-serif; color: #161616; background: #ffffff; margin: 0; }
.wrap { max-width: 46rem; margin: 0 auto; padding: 1rem; }
.home { text-align: center; margin-top: 12vh; }
.brand { font-size: 3.5rem; font-weight: 700; letter-spacing: -0.02em; }
.brand b { color: #d81c2f; }
form.search { margin: 1.5rem 0; }
.search input[type=search] { width: 100%; height: 2.5rem; font-size: 1rem;
  padding: 0 0.6rem; border: 1px solid #8d8d8d; vertical-align: middle; box-sizing: border-box; }
.search button { height: 2.5rem; font-size: 1rem; padding: 0 1rem; background: #d81c2f;
  color: #ffffff; border: none; cursor: pointer; vertical-align: middle; }
.meta { color: #525252; font-size: 0.8rem; margin: 0.5rem 0 1rem; }
.result { margin: 0 0 1.1rem; position: relative; padding-left: 1.5rem; }
.result .fav { position: absolute; left: 0; top: 0.25rem; width: 16px; height: 16px; }
.result a.title { color: #0f62fe; font-size: 1.05rem; text-decoration: none; }
.result a.title:hover { text-decoration: underline; }
.result .url { color: #178a3a; font-size: 0.8rem; word-break: break-all; }
.result p { margin: 0.2rem 0 0; color: #333333; font-size: 0.9rem; }
.ophelp { margin-top: 0.75rem; font-size: 0.85rem; color: #555; text-align: left; display: inline-block; }
.ophelp ul { margin: 0.4rem 0 0; padding-left: 1.2rem; }
.ophelp code { background: #f2f2f2; padding: 0 0.25rem; }
.ac-wrap { position: relative; display: inline-block; width: 70%; max-width: 32rem; vertical-align: middle; }
#ac-list { position: absolute; left: 0; right: 0; top: 100%; margin: 0; padding: 0; list-style: none; background: #fff; border: 1px solid #ccc; border-top: none; z-index: 10; text-align: left; }
#ac-list li { padding: 0.35rem 0.6rem; cursor: pointer; }
#ac-list li[aria-selected="true"], #ac-list li:hover { background: #eef; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0; }
.skip { position: absolute; left: -999px; top: 0; background: #0f62fe; color: #fff; padding: 0.6rem 1rem; z-index: 20; }
.skip:focus { left: 0; }
a:focus-visible, button:focus-visible, input:focus-visible, summary:focus-visible { outline: 2px solid #0f62fe; outline-offset: 2px; }
ul.results { list-style: none; margin: 0; padding: 0; }
nav.pager { display: flex; align-items: center; gap: 1rem; margin: 1.5rem 0 0.5rem; font-size: 0.9rem; }
nav.pager a { color: #0f62fe; text-decoration: none; display: inline-block; padding: 0.5rem 0.75rem; }
nav.pager a:hover { text-decoration: underline; }
nav.pager .page { color: #525252; }
.foot { color: #8d8d8d; font-size: 0.7rem; margin-top: 2rem; border-top: 1px solid #e0e0e0; padding-top: 0.6rem; }
.prov { border: 1px solid #c6c6c6; border-radius: 3px; padding: 0 0.3rem; font-size: 0.72rem; color: #525252; }
.prov-local { border-color: #a7f0ba; background: #defbe6; color: #0e6027; }
.prov-peer { border-color: #bae6ff; background: #e5f6ff; color: #003a6d; }
.prov-ddgs { border-color: #ffd6e8; background: #fff0f7; color: #740937; }
.tabs { margin: 0.25rem 0 0.5rem; font-size: 0.85rem; }
.tabs a, .tabs span[aria-current] { display: inline-block; padding: 0.35rem 0.7rem; color: #0f62fe; text-decoration: none; }
.tabs span[aria-current] { color: #161616; border-bottom: 2px solid #d81c2f; }
.imggrid { display: grid; grid-template-columns: repeat(auto-fill, minmax(9rem, 1fr)); gap: 0.6rem; list-style: none; margin: 0; padding: 0; }
.imggrid img { width: 100%; height: 7rem; object-fit: cover; background: #f4f4f4; display: block; }
.imggrid figcaption { font-size: 0.7rem; color: #525252; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.lightbox { display: none; position: fixed; inset: 0; background: rgba(0,0,0,0.85); z-index: 30; padding: 2rem; text-align: center; }
.lightbox:target { display: block; }
.lightbox img { max-width: 90vw; max-height: 80vh; width: auto; height: auto; object-fit: contain; }
.lightbox a.close { position: absolute; top: 1rem; right: 1.5rem; color: #fff; font-size: 1.5rem; text-decoration: none; }
.lightbox .from { color: #c6c6c6; font-size: 0.85rem; margin-top: 0.6rem; }
.lightbox .from a { color: #78a9ff; }
.facets { margin: 0.5rem 0 1rem; }
.facets summary { cursor: pointer; color: #525252; font-size: 0.85rem; padding: 0.4rem 0; }
.facets fieldset { border: 1px solid #e0e0e0; margin: 0 0 0.6rem; padding: 0.3rem 0.6rem 0.5rem; }
.facets legend { font-size: 0.75rem; color: #525252; padding: 0 0.25rem; }
.facets ul { list-style: none; margin: 0; padding: 0; font-size: 0.8rem; }
.facets li { margin: 0.15rem 0; }
.facets .count { color: #8d8d8d; }
@media (min-width: 48rem) {
  .serp-grid { display: grid; grid-template-columns: 14rem 1fr; gap: 1.5rem; align-items: start; }
  .facets details { display: block; }
  .facets summary { display: none; }
  .facets details[open] { display: block; }
}
.skel { margin: 1rem 0 1.4rem; }
.skel-line { height: 0.85rem; margin: 0.4rem 0; border-radius: 3px; background: linear-gradient(90deg, #e8e8e8 25%, #f4f4f4 50%, #e8e8e8 75%); background-size: 200% 100%; animation: skel-shimmer 1.2s linear infinite; }
.skel-title { width: 55%; height: 1.05rem; }
.skel-short { width: 70%; }
@keyframes skel-shimmer { to { background-position: -200% 0; } }
@media (prefers-reduced-motion: reduce) { .skel-line { animation: none; } }
@media (max-width: 48rem) {
  .search input[type=search], .search button { height: 2.75rem; }
  .topbar { position: sticky; top: 0; background: #ffffff; padding: 0.25rem 0 0.4rem; z-index: 5; box-shadow: 0 1px 0 #e0e0e0; }
  .topbar .ophelp { display: none; }
  nav.pager { justify-content: space-between; }
  nav.pager a { padding: 0.75rem 1rem; }
}
@media (max-width: 32rem) { .brand { font-size: 2.5rem; } .ac-wrap { width: 60%; } }
`
