package extractfetch

import (
	"context"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Page is a fetched HTML page with its outgoing links, the shape the
// Tavily-compatible /crawl and /map endpoints walk.
type Page struct {
	Title string
	Text  string
	Links []string
}

const pageMaxLinks = 200

// FetchPage retrieves the URL like Fetch and additionally collects the page's
// absolute http(s) links (fragments dropped, bounded).
func (f *Fetcher) FetchPage(ctx context.Context, rawURL string) (Page, error) {
	doc, err := f.fetchDocument(ctx, rawURL)
	if err != nil {
		return Page{}, err
	}
	content := extract(doc)

	return Page{
		Title: content.Title,
		Text:  content.Text,
		Links: collectLinks(doc, rawURL),
	}, nil
}

// collectLinks resolves every anchor href against the page URL.
func collectLinks(doc *html.Node, rawURL string) []string {
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	links := make([]string, 0, 32)
	seen := make(map[string]bool)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if len(links) >= pageMaxLinks {
			return
		}
		if node.Type == html.ElementNode && node.Data == "a" {
			if link := anchorLink(node, base); link != "" && !seen[link] {
				seen[link] = true
				links = append(links, link)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	return links
}

// anchorLink resolves one anchor's href into an absolute http(s) URL.
func anchorLink(node *html.Node, base *url.URL) string {
	for _, attr := range node.Attr {
		if attr.Key != "href" {
			continue
		}
		resolved, err := base.Parse(strings.TrimSpace(attr.Val))
		if err != nil {
			return ""
		}
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			return ""
		}
		resolved.Fragment = ""

		return resolved.String()
	}

	return ""
}
