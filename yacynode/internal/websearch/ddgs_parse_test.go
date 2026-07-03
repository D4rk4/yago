package websearch

import "testing"

const listFixture = `<!doctype html><html><body>
<ul class="results-standard">
<li><h2><a href="https://example.com/page">First Result</a></h2><p class="s">A snippet describing the first result.</p></li>
<li><h2><a href="https://direct.example.org/">Second Result</a></h2></li>
<li><h2><a href="/relative-only">Ad without absolute URL</a></h2></li>
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
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2 (relative dropped): %#v", len(results), results)
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
