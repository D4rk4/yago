package websearch

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// parseListResults extracts hits from engines (Mojeek, Bing) that render each
// result as a list item with an `<h2><a href>` title and a `<p>` snippet, using
// direct result URLs. It is structure-driven so it survives class renames.
func parseListResults(body []byte) ([]Result, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse results: %w", err)
	}
	var results []Result
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "li" {
			if result, ok := listItemResult(node); ok {
				results = append(results, result)

				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	return results, nil
}

func listItemResult(item *html.Node) (Result, bool) {
	link := findDescendant(item, func(node *html.Node) bool {
		return node.Data == "a" && node.Parent != nil && node.Parent.Data == "h2"
	})
	if link == nil {
		return Result{}, false
	}
	href, _ := elementAttr(link, "href")
	target := decodeBingRedirect(absoluteURL(href))
	if target == "" {
		return Result{}, false
	}
	result := Result{Title: textContent(link), URL: target}
	if snippet := findDescendant(item, func(node *html.Node) bool {
		return node.Data == "p"
	}); snippet != nil {
		result.Snippet = textContent(snippet)
	}

	return result, true
}

// parseDuckDuckGoResults extracts hits from the html.duckduckgo.com endpoint,
// pairing each result container's link with its snippet and unwrapping the
// `/l/?uddg=` redirector.
func parseDuckDuckGoResults(body []byte) ([]Result, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse ddg html: %w", err)
	}
	var results []Result
	forEachElement(doc, func(node *html.Node) {
		if node.Data != "div" || !hasClass(node, "result") {
			return
		}
		link := findDescendant(node, func(candidate *html.Node) bool {
			return candidate.Data == "a" && hasClass(candidate, "result__a")
		})
		if link == nil {
			return
		}
		href, _ := elementAttr(link, "href")
		target := unwrapRedirect(href)
		if target == "" {
			return
		}
		result := Result{Title: textContent(link), URL: target}
		if snippet := findDescendant(node, func(candidate *html.Node) bool {
			return hasClass(candidate, "result__snippet")
		}); snippet != nil {
			result.Snippet = textContent(snippet)
		}
		results = append(results, result)
	})

	return results, nil
}

// parseDuckDuckGoLiteResults extracts hits from the lite.duckduckgo.com endpoint,
// whose flat table pairs links and snippets by document order.
func parseDuckDuckGoLiteResults(body []byte) ([]Result, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse ddg lite: %w", err)
	}
	var links []Result
	var snippets []string
	forEachElement(doc, func(node *html.Node) {
		switch {
		case node.Data == "a" && hasClass(node, "result-link"):
			href, _ := elementAttr(node, "href")
			if target := unwrapRedirect(href); target != "" {
				links = append(links, Result{Title: textContent(node), URL: target})
			}
		case hasClass(node, "result-snippet"):
			snippets = append(snippets, textContent(node))
		}
	})
	for index := range links {
		if index < len(snippets) {
			links[index].Snippet = snippets[index]
		}
	}

	return links, nil
}

func absoluteURL(href string) string {
	if href == "" {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil || !parsed.IsAbs() {
		return ""
	}

	return href
}

// unwrapRedirect resolves a DuckDuckGo result href, unwrapping the `/l/?uddg=`
// redirector to the real destination and rejecting non-absolute links.
func unwrapRedirect(href string) string {
	if href == "" {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if redirect := parsed.Query().Get("uddg"); redirect != "" {
		return redirect
	}
	if parsed.IsAbs() {
		return href
	}

	return ""
}

func forEachElement(node *html.Node, fn func(*html.Node)) {
	if node.Type == html.ElementNode {
		fn(node)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		forEachElement(child, fn)
	}
}

func findDescendant(node *html.Node, pred func(*html.Node) bool) *html.Node {
	if node.Type == html.ElementNode && pred(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findDescendant(child, pred); found != nil {
			return found
		}
	}

	return nil
}

func hasClass(node *html.Node, want string) bool {
	class, ok := elementAttr(node, "class")
	if !ok {
		return false
	}
	for _, field := range strings.Fields(class) {
		if field == want {
			return true
		}
	}

	return false
}

func elementAttr(node *html.Node, key string) (string, bool) {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val, true
		}
	}

	return "", false
}

func textContent(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)

	return strings.Join(strings.Fields(builder.String()), " ")
}
