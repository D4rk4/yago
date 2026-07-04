package pageparse

import (
	"bytes"
	"strings"

	"github.com/go-shiori/dom"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"

	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

var newHTMLCharsetReader = charset.NewReader

var parseHTMLDocument = html.Parse

const (
	maxPageImages   = 32
	imageAltRuneCap = 160
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
	if htmlNode := dom.QuerySelector(root, "html"); htmlNode != nil {
		page.Language = dom.GetAttribute(htmlNode, "lang")
	}
	if title := dom.QuerySelector(root, "title"); title != nil {
		page.Title = strings.TrimSpace(dom.TextContent(title))
	}
	page.Description = readMetaDescription(root)
	for _, name := range []string{"h1", "h2", "h3", "h4", "h5", "h6"} {
		for _, heading := range dom.GetElementsByTagName(root, name) {
			text := collapseSpaces(dom.TextContent(heading))
			if text != "" {
				page.Headings = append(page.Headings, text)
			}
		}
	}
	for _, link := range dom.GetElementsByTagName(root, "a") {
		if href := dom.GetAttribute(link, "href"); href != "" {
			page.Links = append(page.Links, href)
			if hasLinkRelation(dom.GetAttribute(link, "rel"), "nofollow") {
				page.NoFollowLinks = append(page.NoFollowLinks, href)
				continue
			}
			page.FollowableLinks = append(page.FollowableLinks, href)
		}
	}
	page.Images = readImageMetadata(root, rawURL)
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
