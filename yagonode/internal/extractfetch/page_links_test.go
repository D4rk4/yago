package extractfetch

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func TestFetchPageCollectsLinks(t *testing.T) {
	body := `<html><head><title>Links</title></head><body>
		<p>Linkful body text.</p>
		<a href="/docs">Docs</a>
		<a href="https://other.example/away#frag">Away</a>
		<a href="mailto:x@example.org">Mail</a>
		<a href="javascript:void(0)">JS</a>
		<a href="%zz">Broken</a>
		<a href="/docs">Duplicate</a>
		<a name="anchor-without-href">None</a>
	</body></html>`
	page, err := New(htmlClient(body, "text/html"), time.Second, 0).
		FetchPage(context.Background(), "https://site.example/base/")
	if err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if page.Title != "Links" || !strings.Contains(page.Text, "Linkful body text.") {
		t.Fatalf("page = %+v", page)
	}
	if len(page.Links) != 2 ||
		page.Links[0] != "https://site.example/docs" ||
		page.Links[1] != "https://other.example/away" {
		t.Fatalf("links = %v", page.Links)
	}
}

func TestFetchPageBoundsAndErrors(t *testing.T) {
	var many strings.Builder
	many.WriteString("<html><body>")
	for i := 0; i < pageMaxLinks+50; i++ {
		many.WriteString(`<a href="/p` + strconv.Itoa(i) + `">l</a>`)
	}
	many.WriteString("</body></html>")
	page, err := New(htmlClient(many.String(), "text/html"), time.Second, 0).
		FetchPage(context.Background(), "https://site.example/")
	if err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if len(page.Links) != pageMaxLinks {
		t.Fatalf("links cap = %d, want %d", len(page.Links), pageMaxLinks)
	}

	if _, err := New(htmlClient("x", "application/pdf"), time.Second, 0).
		FetchPage(context.Background(), "https://site.example/"); err == nil {
		t.Fatal("non-html page must fail")
	}

	// An unparsable page URL yields no links.
	doc, err := html.Parse(strings.NewReader(`<a href="/x">l</a>`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if links := collectLinks(doc, "://bad"); links != nil {
		t.Fatalf("bad base links = %v", links)
	}
}
