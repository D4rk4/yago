package websearch

import (
	"strings"
	"testing"
)

const listFixture = `<!doctype html><html><body>
<ul class="results-standard">
<li><h2><a href="https://example.com/page">First Result</a></h2><p class="s">A snippet describing the first result.</p></li>
<li><h2><a href="https://direct.example.org/">Second Result</a></h2></li>
<li><h2><a href="/relative-only">Ad without absolute URL</a></h2></li>
<li><h2><a href="https://www.bing.com/ck/a?!&amp;&amp;p=deadbeef&amp;u=a1aHR0cHM6Ly90YXJnZXQuZXhhbXBsZS5vcmcvcGFnZQ">Bing Tracked Result</a></h2><p>Bing snippet.</p></li>
<li><h2><a href="https://www.bing.com/ck/a?!&amp;&amp;p=deadbeef">Bing redirect without target</a></h2></li>
</ul>
</body></html>`

const ddgFixture = `<!doctype html><html><body>
<div class="result results_links web-result">
  <div class="links_main">
    <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage&rut=abc">DDG Result</a>
    <a class="result__snippet">A DuckDuckGo snippet.</a>
  </div>
</div>
</body></html>`

const ddgLiteFixture = `<!doctype html><html><body>
<table>
<tr><td><a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Flite.example.org%2Fa">Lite One</a></td></tr>
<tr><td class="result-snippet">Lite snippet one.</td></tr>
</table>
</body></html>`

func TestParseListResults(t *testing.T) {
	results, err := parseListResults([]byte(listFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf(
			"results = %d, want 3 (relative and undecodable redirect dropped): %#v",
			len(results),
			results,
		)
	}
	if results[0].Title != "First Result" || results[0].URL != "https://example.com/page" {
		t.Errorf("result[0] = %#v", results[0])
	}
	if results[0].Snippet != "A snippet describing the first result." {
		t.Errorf("result[0] snippet = %q", results[0].Snippet)
	}
	if results[1].URL != "https://direct.example.org/" || results[1].Snippet != "" {
		t.Errorf("result[1] = %#v", results[1])
	}
	if results[2].URL != "https://target.example.org/page" ||
		results[2].Title != "Bing Tracked Result" {
		t.Errorf("bing redirect not decoded: %#v", results[2])
	}
}

func TestParseDuckDuckGoResults(t *testing.T) {
	results, err := parseDuckDuckGoResults([]byte(ddgFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com/page" {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Snippet != "A DuckDuckGo snippet." {
		t.Errorf("snippet = %q", results[0].Snippet)
	}
}

func TestParseDuckDuckGoLiteResults(t *testing.T) {
	results, err := parseDuckDuckGoLiteResults([]byte(ddgLiteFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://lite.example.org/a" {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Snippet != "Lite snippet one." {
		t.Errorf("snippet = %q", results[0].Snippet)
	}
}

func TestAbsoluteURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/x": "https://example.com/x",
		"/relative":             "",
		"":                      "",
	}
	for href, want := range cases {
		if got := absoluteURL(href); got != want {
			t.Errorf("absoluteURL(%q) = %q, want %q", href, got, want)
		}
	}
}

func TestUnwrapRedirect(t *testing.T) {
	cases := map[string]string{
		"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fx&rut=z": "https://example.com/x",
		"https://direct.example.com/y":                                 "https://direct.example.com/y",
		"/relative":                                                    "",
	}
	for href, want := range cases {
		if got := unwrapRedirect(href); got != want {
			t.Errorf("unwrapRedirect(%q) = %q, want %q", href, got, want)
		}
	}
}

const braveFixture = `<!doctype html><html><body>
<div class="snippet svelte-x" data-pos="0" data-type="web">
  <a href="https://www.youtube.com/watch?v=5KC-iscJtsI" class="l1">
    <div class="site-name-content"><div class="text-ellipsis">YouTube</div></div>
    <div class="title snippet-title-hash">ДДТ - Что такое осень (Official video) - YouTube</div>
  </a>
  <div class="generic-content">(С) Navigator Records, 2011. Музыка и слова Ю. Шевчук. Режиссер Б. Деденев. 1991 год.</div>
</div>
<div class="snippet svelte-x" data-pos="1" data-type="videos">
  <a href="https://video.example/ignored"><div class="snippet-title">видеокарусель — не веб-результат</div></a>
</div>
<div class="snippet svelte-x" data-pos="2" data-type="web">
  <a href="https://sefon.pro/mp3/31774-ddt-chto-takoe-osen/" class="l1">
    <div class="snippet-title">ДДТ - Что Такое Осень - скачать песню</div>
  </a>
  <div class="generic-content">Скачать бесплатно ДДТ - Что Такое Осень в качестве 320 kbps. Возможность слушать песню онлайн.</div>
</div>
</body></html>`

func TestParseBraveResults(t *testing.T) {
	results, err := parseBraveResults([]byte(braveFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2 web blocks (the video carousel is skipped)", len(results))
	}
	first := results[0]
	if first.URL != "https://www.youtube.com/watch?v=5KC-iscJtsI" ||
		!strings.Contains(first.Title, "Что такое осень") {
		t.Fatalf("first = %+v", first)
	}
	if !strings.Contains(first.Snippet, "Navigator Records") {
		t.Fatalf("snippet not taken from the long leaf text: %q", first.Snippet)
	}
	if strings.Contains(results[1].Snippet, "Что Такое Осень - скачать песню") &&
		results[1].Snippet == results[1].Title {
		t.Fatalf("snippet must not repeat the title: %+v", results[1])
	}
}
