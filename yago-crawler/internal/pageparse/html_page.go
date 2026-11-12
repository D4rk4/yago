package pageparse

import (
	"mime"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"golang.org/x/net/html"
)

const (
	maximumOutboundLinks = 1024
	maximumFeedLinks     = 16
)

type OutboundLink struct {
	URL      string
	Text     string
	NoFollow bool
}

type HTMLPage struct {
	URL          string
	CanonicalURL string
	Title        string
	Headings     []string
	Language     string
	Text         string
	Links        []OutboundLink
	Noindex      bool
	Nofollow     bool
}

func ParseHTMLPage(rawURL string, body []byte) HTMLPage {
	page := HTMLPage{URL: rawURL}
	base, ok := parseBase(rawURL)
	if !ok {
		return page
	}
	root, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return page
	}
	seenLinks := make(map[string]struct{})
	seenFeeds := make(map[string]struct{})
	var visibleText strings.Builder
	var visit func(*html.Node, bool)
	visit = func(node *html.Node, hidden bool) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "noscript", "template", "svg":
				hidden = true
			}
			inspectPageElement(base, node, &page, seenLinks, seenFeeds)
		}
		if node.Type == html.TextNode && !hidden {
			visibleText.WriteString(node.Data)
			visibleText.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child, hidden)
		}
	}
	visit(root, false)
	if page.Nofollow {
		for index := range page.Links {
			page.Links[index].NoFollow = true
		}
	}
	page.Text = strings.Join(strings.Fields(visibleText.String()), " ")
	return page
}

func inspectPageElement(
	base *url.URL,
	node *html.Node,
	page *HTMLPage,
	seenLinks, seenFeeds map[string]struct{},
) {
	switch node.Data {
	case "html":
		page.Language = attribute(node, "lang")
	case "title":
		if page.Title == "" {
			page.Title = strings.TrimSpace(nodeText(node))
		}
	case "h1", "h2", "h3", "h4", "h5", "h6":
		heading := strings.Join(strings.Fields(nodeText(node)), " ")
		if heading != "" && len(page.Headings) < 128 {
			page.Headings = append(page.Headings, heading)
		}
	case "meta":
		if strings.EqualFold(attribute(node, "name"), "robots") {
			for _, directive := range strings.Split(attribute(node, "content"), ",") {
				switch strings.ToLower(strings.TrimSpace(directive)) {
				case "noindex":
					page.Noindex = true
				case "nofollow":
					page.Nofollow = true
				}
			}
		}
	case "a":
		if len(page.Links) == maximumOutboundLinks {
			return
		}
		resolved, ok := resolveLink(base, attribute(node, "href"))
		if !ok {
			return
		}
		if _, exists := seenLinks[resolved]; exists {
			return
		}
		seenLinks[resolved] = struct{}{}
		page.Links = append(page.Links, OutboundLink{
			URL:      resolved,
			Text:     strings.Join(strings.Fields(nodeText(node)), " "),
			NoFollow: page.Nofollow || hasRelation(attribute(node, "rel"), "nofollow"),
		})
	case "link":
		relation := attribute(node, "rel")
		if hasRelation(relation, "canonical") && page.CanonicalURL == "" {
			page.CanonicalURL, _ = resolveLink(base, attribute(node, "href"))
		}
		if len(page.Links) == maximumOutboundLinks || len(seenFeeds) == maximumFeedLinks ||
			!hasRelation(relation, "alternate") || !isFeedType(attribute(node, "type")) {
			return
		}
		resolved, ok := resolveLink(base, attribute(node, "href"))
		if !ok {
			return
		}
		if _, exists := seenFeeds[resolved]; exists {
			return
		}
		seenFeeds[resolved] = struct{}{}
		page.Links = append(page.Links, OutboundLink{
			URL:      resolved,
			NoFollow: page.Nofollow || hasRelation(relation, "nofollow"),
		})
	}
}

func parseBase(rawURL string) (*url.URL, bool) {
	canonical, ok := yagocrawlcontract.CanonicalURL(rawURL)
	if !ok {
		return nil, false
	}
	base, err := url.Parse(canonical)
	return base, err == nil && base.User == nil
}

func resolveLink(base *url.URL, raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}
	relative, err := url.Parse(value)
	if err != nil || relative.User != nil {
		return "", false
	}
	resolved := base.ResolveReference(relative)
	canonical, ok := yagocrawlcontract.CanonicalURL(resolved.String())
	return canonical, ok
}

func isFeedType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(mediaType, "application/rss+xml") ||
		strings.EqualFold(mediaType, "application/atom+xml")
}

func hasRelation(value, relation string) bool {
	for _, part := range strings.Fields(value) {
		if strings.EqualFold(part, relation) {
			return true
		}
	}
	return false
}

func attribute(node *html.Node, name string) string {
	for _, item := range node.Attr {
		if strings.EqualFold(item.Key, name) {
			return item.Val
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	var text strings.Builder
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			text.WriteString(current.Data)
			text.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return text.String()
}
