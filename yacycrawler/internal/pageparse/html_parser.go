package pageparse

import (
	"bytes"
	"strings"

	"github.com/go-shiori/dom"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"
)

func ParseHTML(rawURL, contentType string, body []byte) crawlwork.ParsedPage {
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		reader = bytes.NewReader(body)
	}
	root, err := html.Parse(reader)
	if err != nil {
		return crawlwork.ParsedPage{URL: rawURL}
	}

	page := crawlwork.ParsedPage{URL: rawURL}
	var text strings.Builder
	readHTMLFields(root, &page)
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

func readHTMLFields(root *html.Node, page *crawlwork.ParsedPage) {
	if htmlNode := dom.QuerySelector(root, "html"); htmlNode != nil {
		page.Language = dom.GetAttribute(htmlNode, "lang")
	}
	if title := dom.QuerySelector(root, "title"); title != nil {
		page.Title = strings.TrimSpace(dom.TextContent(title))
	}
	for _, link := range dom.GetElementsByTagName(root, "a") {
		if href := dom.GetAttribute(link, "href"); href != "" {
			page.Links = append(page.Links, href)
		}
	}
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
