package pageparse

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/go-shiori/dom"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"

	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
)

var newHTMLCharsetReader = charset.NewReader

var parseHTMLDocument = html.Parse

const (
	maxPageImages   = 32
	imageAltRuneCap = 160
	maxPageAnchors  = 1024
	anchorTextCap   = 256
)

func ParseHTML(rawURL, contentType string, body []byte) ParsedPage {
	reader, err := newHTMLCharsetReader(bytes.NewReader(body), contentType)
	if err != nil {
		reader = bytes.NewReader(body)
	}
	root, err := parseHTMLDocument(reader)
	if err != nil {
		return ParsedPage{URL: rawURL}
	}

	page := ParsedPage{URL: rawURL}
	var text strings.Builder
	readHTMLFields(root, rawURL, &page)
	page.PublishedAt, page.ModifiedAt, page.DateConfidence, page.DateSource = readPageDates(root)
	page.SafetyLabels = readSafetyLabels(root)
	page.CanonicalURL = readCanonicalURL(root, rawURL)
	collectText(root, &text)
	page.Text = selectText(contentType, body, text.String())
	return page
}

func selectText(contentType string, body []byte, fallback string) string {
	if main, err := extractMainContent(contentType, body); err == nil && main != "" {
		return main
	}
	return collapseSpaces(fallback)
}

func readHTMLFields(root *html.Node, rawURL string, page *ParsedPage) {
	readDocumentFields(root, page)
	readHeadings(root, page)
	readLinks(root, rawURL, page)
	page.Images = readImageMetadata(root, rawURL)
}

func readDocumentFields(root *html.Node, page *ParsedPage) {
	if htmlNode := dom.QuerySelector(root, "html"); htmlNode != nil {
		page.Language = dom.GetAttribute(htmlNode, "lang")
	}
	if title := dom.QuerySelector(root, "title"); title != nil {
		page.Title = strings.TrimSpace(dom.TextContent(title))
	}
	page.Description = readMetaDescription(root)
	page.Author = readMetaAuthor(root)
	page.Keywords = readMetaKeywords(root)
	page.Publisher = readMetaPublisher(root)
	page.MetaNoindex, page.MetaNofollow = readMetaRobots(root)
}

func readHeadings(root *html.Node, page *ParsedPage) {
	for _, name := range []string{"h1", "h2", "h3", "h4", "h5", "h6"} {
		for _, heading := range dom.GetElementsByTagName(root, name) {
			text := collapseSpaces(dom.TextContent(heading))
			if text != "" {
				page.Headings = append(page.Headings, text)
			}
		}
	}
}

func readLinks(root *html.Node, rawURL string, page *ParsedPage) {
	baseURL, hasBaseURL := weburl.ParseBase(rawURL)
	for _, link := range dom.GetElementsByTagName(root, "a") {
		href := dom.GetAttribute(link, "href")
		if href == "" {
			continue
		}
		page.Links = append(page.Links, href)
		relation := dom.GetAttribute(link, "rel")
		noFollow := page.MetaNofollow || hasLinkRelation(relation, "nofollow")
		if noFollow {
			page.NoFollowLinks = append(page.NoFollowLinks, href)
		} else {
			page.FollowableLinks = append(page.FollowableLinks, href)
		}
		if !hasBaseURL || len(page.OutboundAnchors) == maxPageAnchors {
			continue
		}
		if anchor, ok := outboundAnchorFromLink(baseURL, link, href, noFollow); ok {
			anchor.UserGenerated = hasLinkRelation(relation, "ugc")
			anchor.Sponsored = hasLinkRelation(relation, "sponsored")
			page.OutboundAnchors = append(page.OutboundAnchors, anchor)
		}
	}
}

func outboundAnchorFromLink(
	baseURL *url.URL,
	link *html.Node,
	href string,
	noFollow bool,
) (OutboundAnchor, bool) {
	resolved, ok := weburl.Resolve(baseURL, href)
	if !ok {
		return OutboundAnchor{}, false
	}
	targetURL, ok := weburl.Normalize(resolved.String())
	if !ok {
		return OutboundAnchor{}, false
	}
	text := collapseSpaces(dom.TextContent(link))
	if text == "" {
		text = collapseSpaces(dom.GetAttribute(link, "aria-label"))
	}
	if text == "" {
		text = collapseSpaces(dom.GetAttribute(link, "title"))
	}
	runes := []rune(text)
	if len(runes) > anchorTextCap {
		text = string(runes[:anchorTextCap])
	}

	return OutboundAnchor{TargetURL: targetURL, Text: text, NoFollow: noFollow}, true
}

func readCanonicalURL(root *html.Node, rawURL string) string {
	base, ok := weburl.ParseBase(rawURL)
	if !ok {
		return ""
	}
	for _, link := range dom.GetElementsByTagName(root, "link") {
		if !hasLinkRelation(dom.GetAttribute(link, "rel"), "canonical") {
			continue
		}
		resolved, ok := weburl.Resolve(base, dom.GetAttribute(link, "href"))
		if !ok {
			continue
		}
		if normalized, ok := weburl.Normalize(resolved.String()); ok {
			return normalized
		}
	}
	return ""
}

// readMetaAuthor reads the page author from the conventional meta tags, in
// order of specificity: article:author, then name=author.
func readMetaAuthor(root *html.Node) string {
	byName := ""
	for _, meta := range dom.GetElementsByTagName(root, "meta") {
		content := collapseSpaces(dom.GetAttribute(meta, "content"))
		if content == "" {
			continue
		}
		if strings.EqualFold(dom.GetAttribute(meta, "property"), "article:author") {
			return content
		}
		if byName == "" && strings.EqualFold(dom.GetAttribute(meta, "name"), "author") {
			byName = content
		}
	}

	return byName
}

// readMetaKeywords reads the page's comma-separated keywords from the
// conventional <meta name="keywords"> tag, surfaced as the RSS dc:subject field.
func readMetaKeywords(root *html.Node) string {
	for _, meta := range dom.GetElementsByTagName(root, "meta") {
		if !strings.EqualFold(dom.GetAttribute(meta, "name"), "keywords") {
			continue
		}
		if content := collapseSpaces(dom.GetAttribute(meta, "content")); content != "" {
			return content
		}
	}

	return ""
}

// readMetaPublisher reads the page publisher from the conventional meta tags, in
// order of specificity: name=publisher, then property=og:site_name. It feeds the
// RSS dc:publisher field.
func readMetaPublisher(root *html.Node) string {
	fallback := ""
	for _, meta := range dom.GetElementsByTagName(root, "meta") {
		content := collapseSpaces(dom.GetAttribute(meta, "content"))
		if content == "" {
			continue
		}
		if strings.EqualFold(dom.GetAttribute(meta, "name"), "publisher") {
			return content
		}
		if fallback == "" &&
			strings.EqualFold(dom.GetAttribute(meta, "property"), "og:site_name") {
			fallback = content
		}
	}

	return fallback
}

func readMetaDescription(root *html.Node) string {
	for _, meta := range dom.GetElementsByTagName(root, "meta") {
		if !strings.EqualFold(dom.GetAttribute(meta, "name"), "description") {
			continue
		}
		if description := collapseSpaces(dom.GetAttribute(meta, "content")); description != "" {
			return description
		}
	}
	return ""
}

func readImageMetadata(root *html.Node, rawURL string) []ImageMetadata {
	base, ok := weburl.ParseBase(rawURL)
	if !ok {
		return nil
	}
	images := make([]ImageMetadata, 0)
	for _, img := range dom.GetElementsByTagName(root, "img") {
		if len(images) >= maxPageImages {
			break
		}
		src := strings.TrimSpace(dom.GetAttribute(img, "src"))
		if src == "" {
			continue
		}
		resolved, ok := weburl.Resolve(base, src)
		if !ok {
			continue
		}
		normalized, ok := weburl.Normalize(resolved.String())
		if !ok {
			continue
		}
		images = append(images, ImageMetadata{
			URL:     normalized,
			AltText: boundedImageAltText(dom.GetAttribute(img, "alt")),
		})
	}
	return images
}

func boundedImageAltText(text string) string {
	text = collapseSpaces(text)
	runes := []rune(text)
	if len(runes) <= imageAltRuneCap {
		return text
	}

	return string(runes[:imageAltRuneCap])
}

func collectText(node *html.Node, text *strings.Builder) {
	switch node.Type {
	case html.ElementNode:
		switch node.DataAtom {
		case atom.Script, atom.Style, atom.Noscript, atom.Template:
			return
		}
	case html.TextNode:
		text.WriteString(node.Data)
		text.WriteByte(' ')
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectText(child, text)
	}
}

func collapseSpaces(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
