package pageparse_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/pageparse"
)

func goldenHTML(t *testing.T, name string) []byte {
	t.Helper()
	//nolint:gosec // G304: fixed testdata directory with a test-controlled fixture name.
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}

	return data
}

func TestParseHTMLGoldenRelativeLinks(t *testing.T) {
	page := pageparse.ParseHTML(
		"https://example.org/dir/page.html",
		"text/html",
		goldenHTML(t, "relative_links.html"),
	)
	if page.Title != "Relative Links" {
		t.Fatalf("title = %q", page.Title)
	}
	if len(page.Links) != 7 {
		t.Fatalf("links = %d, want 7: %v", len(page.Links), page.Links)
	}

	local, external := pageparse.ResolveLinks("https://example.org/dir/page.html", page.Links)
	if len(local) != 5 {
		t.Fatalf("local = %d, want 5: %v", len(local), local)
	}
	if len(external) != 1 || external[0] != "https://other.example/x" {
		t.Fatalf("external = %v, want [https://other.example/x]", external)
	}
	if !slices.Contains(local, "https://example.org/dir/sub/child.html") {
		t.Fatalf("local missing resolved relative link: %v", local)
	}
}

func TestResolveLinksSplitsHostsAndSkipsNonHTTP(t *testing.T) {
	local, external := pageparse.ResolveLinks("https://example.org/dir/page.html", []string{
		"sub/child.html", "/root.html", "../up.html",
		"https://example.org/abs", "https://other.example/x", "mailto:a@b", "#frag",
	})
	if len(local) != 5 || len(external) != 1 {
		t.Fatalf("local=%v external=%v", local, external)
	}
	for _, want := range []string{
		"https://example.org/dir/sub/child.html",
		"https://example.org/root.html",
		"https://example.org/up.html",
	} {
		if !slices.Contains(local, want) {
			t.Fatalf("local %v missing %q", local, want)
		}
	}
	if external[0] != "https://other.example/x" {
		t.Fatalf("external = %v", external)
	}
}

func TestResolveLinksRejectsBadBase(t *testing.T) {
	if local, external := pageparse.ResolveLinks("://bad-base", []string{"/x"}); local != nil ||
		external != nil {
		t.Fatalf("bad base should yield nil, got %v %v", local, external)
	}
}

func TestParseHTMLGoldenMalformed(t *testing.T) {
	page := pageparse.ParseHTML(
		"http://example.com/",
		"text/html",
		goldenHTML(t, "malformed.html"),
	)
	if page.Title != "Broken Page" {
		t.Fatalf("title = %q", page.Title)
	}
	if !slices.Contains(page.Links, "/rel/link") {
		t.Fatalf("links = %v, want to include /rel/link", page.Links)
	}
	if len(page.Headings) == 0 || page.Headings[0] != "Orphan Heading" {
		t.Fatalf("headings = %v", page.Headings)
	}
	if len(page.Images) != 1 || page.Images[0].AltText != "a picture" {
		t.Fatalf("images = %v", page.Images)
	}
}

func TestParseHTMLGoldenUTF8Multibyte(t *testing.T) {
	page := pageparse.ParseHTML(
		"http://example.com/",
		"text/html; charset=utf-8",
		goldenHTML(t, "utf8_multibyte.html"),
	)
	if !strings.Contains(page.Title, "Привет") || !strings.Contains(page.Title, "☕") {
		t.Fatalf("title = %q", page.Title)
	}
	if page.Language != "ru" {
		t.Fatalf("language = %q", page.Language)
	}
	if len(page.Headings) == 0 || page.Headings[0] != "Über Größe" {
		t.Fatalf("headings = %v", page.Headings)
	}
}

func TestParseHTMLNonUTF8ViaContentTypeHeader(t *testing.T) {
	body := []byte(
		"<html><head><title>Caf\xe9 R\xe9sum\xe9</title></head><body><p>na\xefve</p></body></html>",
	)
	page := pageparse.ParseHTML("http://example.com/", "text/html; charset=windows-1252", body)
	if page.Title != "Café Résumé" {
		t.Fatalf("title = %q, want windows-1252 decoded via header", page.Title)
	}
}
